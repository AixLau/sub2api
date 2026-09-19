# AT-40 跨入口凭证互斥与 UNKNOWN 别名冲突

`report_revision=at40-arbitration-r1`。基线和起点HEAD为 `a2122fcba84f1defe65e51605cadefdf45f4edfd`，分支 `feat/multi-credential-http`，起点仅原有未跟踪 `backend/internal/service/zz_debug_test.go`。该文件未改、未提交，受测Git追踪快照不包含它。本轮只处理两个AT-40缺陷，没有修改admission调度、并发预算、期限、WS、session/full、UA/TLS实现或既有隐私清理；不访问生产。

## 基线实际复现

真实PG测试以 `a2122fcba` 的git archive加单独测试文件overlay执行，命令、overlay SHA及原始失败记录在 [证据JSON](at40-test-results.json)。两个预期安全断言均实际FAIL，退出1：

1. legacy KnownCheck返回false后在channel barrier暂停，受控PutImport提交，再允许旧SQL writer写入；两边密文/账号凭证是同一token，legacy仍可调度。基线SQL用于锁定原check/write间隙；修复后回归改用正式配置的AccountRepository.Create，不能将应用层事务宣称为任意SQL访问控制。
2. A的UNKNOWN结果新token指纹碰撞retired R历史owner；A的密文和UNKNOWN状态保留、历史owner仍R，但KnownCheck返回false。修复后独立声明使KnownCheck返回true。

这是本地SQL行为的动态复现，不是观察到真实provider生成重复token或上游超限。provider身份及compact契约仍BLOCKED。

## 修改与原子边界

缺陷2由 `e2c9445a1` / `7022ca16c` 独立修复：前向迁移262增加instance alias claims及refresh operation alias claims。历史credential_fingerprints的首个owner不覆盖；同一个指纹可保留多个当前声明。SENDING/REFRESH_RESULT_UNKNOWN没有TTL自动解除。未知结果密文与别名同一事务持久化，即使历史owner退休也不能漏阻断。Complete只结束自身op，其他UNKNOWN/实例声明保留；没有专门“超时后解锁”API。

缺陷1由 `a8caba62b` 与后续 `1536849f2` 修复：迁移263增加稳定的凭证变更仲裁行、token registry、legacy account claims以及legacy refresh operation/aliases。所有参与的凭证变更短事务先锁同一仲裁行，因此没有指纹行时也能互斥；多指纹按排序去重后登记和锁定。之后保留各自原有principal→instance→operation、account和group相对锁序。部分指纹集合只能在domain读取后确定，此时仍由同一个前置仲裁行排除其他凭证writer；不谎称所有路径都是token行先于domain。admission、Heartbeat、Finish执行账本不取得该锁，网络期间也不持有它。这是控制面正确性边界，不提供凭证变更吞吐承诺。

正式Wire使用配置化AccountRepository，Create/CreateWithAccountGroups/updateAccount/UpdateCredentials/BulkUpdate在同一个Ent事务中核查和记录归属、写账号及outbox；复用调用者Ent事务时不越过它另开pool写入。CRS、JSON import、admin编辑/复制、刷新保存都汇聚到这些写入口。内部未配置构造器仅供原有测试夹具使用，已移除旧导出的无配置构造入口。service KnownCheck保留为提前拒绝，绝不作为写入许可。

普通受控导入允许暂存已有legacy账号的token供离线迁移准备，但不授予第二个carrier：CreatePrincipal/AddInstance拒绝仍有legacy或UNKNOWN声明的凭证。只有Migrate经真实fence、指定原账号及已验证import一致检查后，才在事务中把**该账号全部已知历史别名**交给新instance，并标记原claims已转移。回滚只恢复最新明文，全部历史别名回到原账号；旧别名保留spent阻断，不恢复旧token。其他UNKNOWN声明冲突时回滚保持暂停。不存在通过管理员填写主体字段替代provider证明。

## 旧 HTTP 刷新及恢复

raw/manual/token worker仍汇聚到RefreshTokenWithClientID，但provider之前必须提交持久SENDING操作及输入指纹。只有收到新owner的明确提交成功才发送；Begin提交确认丢失不外呼，后续查询看到SENDING仍阻断。HTTP mock被barrier挂起时，另一个真实PG事务能够立即取得仲裁行，验证没有跨网络长事务。

provider成功返回后，在JWT解析和订阅补充之前保存加密TokenResponse、首次received_at、输出aliases及SUCCEEDED状态；同一输入重试只返回同一密文结果，ExpiresAt不顺延。provider error/panic、完成失败或结果未知时，用独立3秒补偿保留UNKNOWN，收到的新token密文不能因别名冲突、管理员换凭证或删除账号而丢弃。操作身份/owner nonce核查保留；成功提交ACK丢失后的补偿不会把SUCCEEDED退回UNKNOWN。不存在自动重放上游。

最终Account写入也核对该成功结果的预期原始凭证与当前文档，防止Finish之后管理员换凭证、迟到worker再覆盖。批量两个账号赋同一未登记token在同一事务内冲突，整批回滚。已持久化的刷新输入一旦被轮换，旧文档不能把spent token写回。

如果provider省略refresh_token，保持现有返回字段和合并语义：原输入refresh别名继续声明为输出，返回/receipt不伪造新refresh字段。同一refresh输入成功后仅可取回原结果；原结果到期会返回LEGACY_REFRESH_RESULT_EXPIRED，**不会假定provider支持安全再次使用相同token**。需要新授权或另有可信契约后才能变更此默认；这是一项明确的可用性限制，不是每个provider均完成刷新兼容验收。

## 存量、密钥与升级

配置化启动使用稳定HMAC key，在凭证变更仲裁事务内索引现有legacy tokens，并解密回填历史UNKNOWN结果的独立别名；不修改Account ID、token、profile、安装标识、权限、计费字段或占用。密文损坏、key不一致、已存在legacy/controlled双有效冲突均拒绝初始化，不留下部分索引/initialized记录，不自动修复为可用。纯SQL复制历史fingerprints不能替代这个回填。

**即使多凭证开关为false，生产旧OpenAI OAuth刷新现在也要求有效vault key和持久操作仓储。** 未配置key时没有裸发回退；纯legacy库在仲裁尚未初始化时仍可进行被仲裁行保护的账号写入，一旦初始化，缺key/错key节点不能继续写入另一个摘要空间。已有受控/UNKNOWN数据缺key时启动拒绝。部署必须事先配置/备份密钥并清点全部凭证writer，不能将默认关闭理解为旧刷新行为完全不变。

历史claims和HMAC在加密密钥轮换时不变。新第五类密文 `credential_legacy_refresh_operations.result_ciphertext` 已接入原离线轮换；SENDING/UNKNOWN状态及输入/输出claims不改变，reverse只重封当前新token结果。迁移262/263均前向，不DROP声明或依靠TTL删除未知操作。

升级需保持功能关闭，离线fence完整网关/worker清单，执行正式迁移并以正确稳定key完成索引。旧二进制不参与新协议，禁止与新writer混跑；新增schema无法让已缓存token的旧节点自动遵守仲裁。回退必须继续fence/暂停，保留全部claims、操作、最新密文和未知占用，只使用支持该协议的程序；不能降回a212旧check-only入口、删表、清状态或还原旧token快照。

## 验收范围与结论

最终产品快照 `1536849f2e714e265b584528fb51be3bf03ea094`，测试/二进制/命令/退出码按 [机器证据](at40-test-results.json) 记录。已实际运行的最终回归覆盖：

- channel与真实pg_stat_activity锁等待barrier的双向检查/写入竞争；指纹行尚不存在的同时到达；反序多指纹输入；五个正式仓储写入口；
- 三个独立OS进程及各自SQL pool，仅一个SENDING owner；owner未Finish退出后第三进程和受控入口仍阻断；这项不冒称模拟了所有主机崩溃；
- 实际本地HTTP mock外呼不持PG事务、provider调用计数不重放，提交成功后驱动返回ACK错误的Begin/Finish注入；
- retired历史owner、两个UNKNOWN碰撞、同op完成及他人claims不清；合法迁移/回滚、全部历史别名保留、删除账号或换凭证后密文恢复；
- stableHMAC和第五类密文轮换；缺key、错key、损坏历史UNKNOWN密文及既有双有效状态安全拒绝。

| 最终命令组 | 实际结果 |
|---|---|
| 定向unit service/admin/repository | 退出0，57根+87子测试PASS |
| 同范围race | 退出0，57根+87子测试PASS，无race报告 |
| 真实PG追加race：HTTP外呼、三进程、无记录/多指纹并发、Begin/Finish丢ACK | 退出0，6根PASS，总69.22秒，无race报告 |
| AT-40真实PG含跨进程/HTTP/barrier/ACK丢失/history | 退出0，22根+9子测试PASS，47.91秒 |
| 独立alias及密钥轮换PG | 退出0，4根PASS，12.05秒 |
| 受控导入/刷新/维护探测/迁移回归 | 退出0，10根PASS，9.42秒 |
| 真实handler全局用户容量、15秒预算、usage/过载恢复 | 退出0，15根+17子PASS，97.88秒 |
| server/mock/repository测试二进制构建 | 全部退出0 |
| 三个完整gateway闭环 | 退出0，61.86秒；8次mock调用、7条usage、1条UNKNOWN占用，未重放，日志秘密扫描通过 |
| govulncheck Go1.27 | 退出0，0可达、0导入包、6仅模块级发现；非全依赖无漏洞 |
| repository/admin/config完整unit包 | 退出1，1159根PASS、2根FAIL及819子PASS；仅两个Dashboard用例失败，a212精确同名补跑两者均FAIL，仍NOT_PASS |

两项Dashboard为TestDashboardNativeCompactionFilterPropagatesAlongsideTransport与TestDashboardNativeCompactionFilterRejectsInvalidBoolean。它们在本轮基线实际退出1，不依靠上轮代表性失败归因，也没有修改其无关产品实现。没有重跑全仓suite；本轮按改动范围验证，上轮全套失败状态不被改成PASS。

原基线两个预期FAIL、共享开发期间缺失接口/unused import编译错误、SQL UUID参数错误、错误期待2条claims实际4条的断言、以及混合夹具假cipher引起初始化拒绝的整条FAIL均保留。最终独立分组通过不覆盖首次失败。最后候选之后只有报告变化，不用旧结果覆盖新源码。

server SHA256 `42887bf7e6b77658f548737830c7ed2d80ae4b128ed221b766531eba59e40485`；repository测试binary `45d59478e497a03c6da012e96128e9a1b8bae67af2969cbdec7eaf5857e44832`。本机darwin/arm64 Go1.27、单PG实际18.4(amd64镜像跨架构)、单Redis；完整gateway为同机Docker普通cmd/server，不作为原生性能证明。命令精确环境与日志散列以证据JSON为准。

原五项依赖漏洞、离线轮换、声明单机三网关闭环及E5容量/生命周期通过证据仍成立，且相关改变由本轮定向回归补证；本轮未改调度、未重跑六组合或重新宣称性能达标。20ms目标与C200利用率缺口继续单独跟踪。

AT-40本轮限定的两个内部旁路缺陷可在上述受管HTTP入口与本地token相等关系范围内关闭，不能扩大为生产整体可启用。外部provider account+user、refresh-family和compact契约仍缺失，真实导入UNVERIFIED；未经本部署登记的远端授权关系不是本地指纹可证明的事实。HA、跨地域双活、Redis Cluster、任意直接DB写入或外部持token执行者不在此协议保证内。UNKNOWN执行容量继续保留并人工核对，不自动零化或重放。

采用现有Ent事务和PostgreSQL稳定锁对象；[PostgreSQL锁文档](https://www.postgresql.org/docs/current/explicit-locking.html)说明行锁只锁实际返回的行，因此用持久仲裁行和token注册行覆盖首次插入，未采用缺行FOR UPDATE作为互斥保证。未新增通用基础设施依赖。
