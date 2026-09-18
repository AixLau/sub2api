# 924819 发布阻断项收尾报告

`report_revision=release-closure-r3`。起点实际HEAD和用户快照均为 `924819c5d04a550801fb4e85441059389dc00caa`，分支 `feat/multi-credential-http`；起点 `git diff 924819c5..HEAD` 为空，仅原有未跟踪 `backend/internal/service/zz_debug_test.go`。该文件未改动、未提交，也未纳入测试快照。保留其他工作流提交，包括 `f416f1a93` 和 migration 260。

已完整读取用户 acceptance_report_924819c5.md、850行原规格、AT账本及证据目录全部Markdown；全部JSON逐对象解析。阅读清单和原文件SHA在 `/tmp/sub2api-release-closure/read-evidence-inventory.json`。旧证据不代替本轮运行。

仅在本机测试，未操作生产。产品默认 `gateway.multi_credential_http_enabled=false`；真实verifier仍nil、导入UNVERIFIED，真实compact契约BLOCKED。隔离测试显式启用mock路径不能作为可信provider证据。未修改WS、session/full、UA/TLS实现或既有隐私清理。

## 受测快照及产物

- 产品代码、非integration测试、持续发生器：`b06789dde6ddb7c25fc29ffb7ae81ed79cf6eef8`，来自仅Git追踪内容的独立快照；3503个追踪backend文件逐一核对无缺失/修改。
- 之后唯一backend差异：`35aff321ddc65457f65fe2cbf3e0d4a257dce304` 的 `credential_rollout_full_gateway_acceptance_test.go`，修复容器restart后测试客户端重连。它有integration build tag，未改变产品、非integration输入、发生器、依赖或迁移；已以新测试二进制独立复验。不能用旧二进制覆盖该修正。
- 其他后续变化为报告及证据解析脚本；解析脚本7项Python测试实际通过。
- 产品server SHA256：`48e0942233b064ebe39d9bb3478ae275fd0d03ea1c54b1ef889e5c157ec3b2b7`。
- E5与初次integration测试二进制：`dff0ea2fa26e3965c1ae64349efb44af29225a2c1925c88ba223a10a5f1117de`；修正后的完整网关fixture二进制：`d8ddcba62ef1440756b04d2aa77e91c9f8723e4d6dab8d0f44800ec49be0f948`。

完整命令、环境、退出码、时间及日志SHA集中在 [release-validation-results.json](release-validation-results.json)。原始日志 `/tmp/sub2api-release-closure/final-logs/`。不追写“文档HEAD就是受测HEAD”；文档版本、产品SHA、发生器SHA和fixture SHA分别记录。

## 每类交付、验证与回滚

| 修改 | 独立提交 | 实际验证及剩余边界 | 迁移与回滚 |
|---|---|---|---|
| grpc 1.83.2、x/image 0.45.0及必要MVS闭包；真实插件子进程/图像回归 | 6f8dc9b36 | 最终定向/race及Go1.27 govulncheck通过；原五项消失，0可达/0导入包/6仅模块级发现。保留xDS/32位等触发条件及外部签名插件SKIP | 无迁移；降回旧依赖会恢复漏洞，不作为生产安全回滚 |
| 受控退避A/B/A、quota读合并、审计与容量原子写合并 | 9046c871e、40b963090 | 同二进制退避减少竞争但call尾部变差，未采用；SQL故障回滚/权限/时间边界及E5六组合通过执行断言，性能仍PARTIAL | 无schema/预算变化；回退代码保留全部账本、receipt、未知状态 |
| stable fingerprint key与加密key分离；四类密文离线重封与启动key-ID检查 | b99cc61f7 | 真实PG错key/中途损坏/fence丢失原子回滚、COMMIT成功ACK丢失、同op恢复及reverse-current-data通过；版本/身份/未知占用保持 | 新增前向261；回退只离线重加密当前事实，禁止旧token快照或DROP状态表 |
| import/refresh/HTTP秘密panic边界；receipt前异常保留UNKNOWN | d5a530051 | provider/Begin/commit/补偿panic及终结后Close panic、fmt/slog/zap/Gin实际回归通过；外置APM/heap/core dump未验证 | 无迁移；回退将重新暴露秘密与无receipt终结风险，不能据此放行 |
| 已登记token在admin/raw refresh/CRS/worker入口被拒绝；UNKNOWN普通新alias保留；原串/规范化串检查 | 4ec7c3e6a、b06789dde | 定向、race、PG普通old/latest/pending alias通过；仍有下列两个内部缺陷，AT-40总体PARTIAL | 无新表；不能把撤掉guard视为安全回滚 |
| 三个完整网关fence→迁移→主体canary→receipt恢复→持久存储restart→UNKNOWN暂停→latest-token回滚 | 22bc1ac77、b9a8b1024、35aff321d | 最终完整闭环PASS；真实fde7旧binary fence也PASS；mock不证明真实provider，state loss/HA不支持 | 正式产品Migrate/Canary/Rollback被实际调用；无额外产品迁移，未终结工作继续阻断回滚 |
| FastPolicy矛盾越界断言与逐测试证据比较器 | 8b1c4a46d、44cdec58c、51280d5d2 | FastPolicy定向PASS，候选default原141中断测试完成；全套仍有实际失败，逐项对照，不以同失败算PASS | 无迁移，不改FastPolicy产品或其他失败业务 |

## 实际测试结论

| 命令/范围 | 实际结果 |
|---|---|
| 冻结候选Linux/amd64 cmd/server、mock、repository integration binary构建 | 退出0 |
| 最终 `go test -p 2 -tags=unit` 定向9包 | 退出0，96根测试+114子测试PASS；外部签名插件用例SKIP |
| 对应service/admin/repository/rotation CLI `-race` | 退出0，无race报告 |
| 924819 / b067 默认完整 `go test -p 2 ./... -count=1 -timeout=15m -json` | 两边退出1；基线56根+23子FAIL/141根未终结，候选62根+37子FAIL/0未终结 |
| 两边完整 `-tags=unit ./...` | 两边退出1，各19根+19子FAIL/49根未终结；另有3个失败包事件 |
| default新增观察8根用例两边同过滤补证 | 两边实际8根+14子FAIL；不再以基线没执行到而推断新增回归 |
| unit service排除两个已知panic的同命令补覆盖 | 两边退出1但均无未终结；基线65根+38子FAIL，候选63根+36子FAIL。排除的失败保留，不能称原suite通过 |
| broad integration首跑及基线同范围 | 两边15根+17子FAIL；审计fixture留RESERVED导致首次Redis epoch初始化安全拒绝 |
| 同候选保存二进制、新TestMain的handler+全局用户+15秒期限+usage+过载恢复 | 退出0，98.12秒，对应15根+17子全PASS；原broad失败不改写 |
| 最终真实旧binary fence / 修正fixture后三完整网关闭环 | 分别退出0，9.10/50.65秒；持久存储restart后epoch及UNKNOWN保持 |
| 最终govulncheck | 首次自动Go1.26加载失败退出1；显式GOTOOLCHAIN=go1.27.0复验退出0，0可达/0导入包/6模块级 |
| 固定六组合各10分钟 E5 | 整条退出0，3677.22秒，共114527次dispatch全部释放，零超限/重复/记录错误；20ms性能目标仍未全部达到 |

失败逐项证据在 [release-failure-attribution.md](release-failure-attribution.md) 及所链JSON。补证范围内没有观察到基线实际PASS→候选FAIL；这不是全套PASS。少数未改WS/异步风险审计用例本次未复现，仍未归因为本轮修复。既有代理日期校验、模型/计费遗漏和复杂未归因失败未被擅自改动。

首次综合integration、Go工具链不匹配和完整网关restart失败均保留。restart复验记录Redis32895→32896、PG32894→32897，纠正的是测试主机连接端口；产品内部database:5432/redis:6379、epoch、TTL和准入规则未改。

## 性能及系统边界

E5成功准入事务p95：C10/I3 18.17ms、C10/I16 18.61ms、C50/I3 23.29ms、C50/I16 25.57ms、C200/I3 21.59ms、C200/I16 22.25ms。C10 WAIT事务仍为21.16/21.25ms。C200 call p95约5.03/5.34秒，平均上游利用率28.00%/27.23%，不能解释为账号容量用满。

C50有接近每次dispatch一轮WAIT及约21万次advisory失败；C200没有WAIT/后台推进，但串行成功事务mean16.81/17.31ms，其累计wall接近测量窗口，且前置等待仍为秒级。分析只用实测均值、提交率、重试与SQL阶段，不以p95倒数推吞吐，不把SQL wall叫纯锁时间。详见 [E5报告](release-endurance-E5.md) 和 [受控A/B/A](release-admission-cost.md)。本机arm64运行amd64 PG限制绝对性能外推，未以该条件豁免未达标。

完整网关闭环采用单PG主库、共享单Redis、同机每轮3个普通cmd/server；可靠receipt重启后11.014秒完成本地释放和结算，持久存储restart续期11.131秒。总上游8次、usage7条、UNKNOWN占用1，未重放；15份完整进程日志秘密扫描通过。常规持久重启不是Redis全部丢失/备份还原/HA恢复。安装标识数量不是provider设备数。

## 三个交付结论与生产条件

**代码实现状态：** 原五项安全依赖缺陷已修复，局部SQL开销降低，密钥轮换与秘密边界、已登记别名入口保护及完整拓扑验收已补齐。仍有两个内部生产阻断：legacy Check与导入登记/外呼之间无共同锁的TOCTOU；UNKNOWN新alias与已retired旧owner冲突可能漏阻断。前者有静态可构造交错，后者是SQL静态条件性风险且未动态复现。详见 [秘密操作报告](credential-secret-operations.md)。不能把它们归为外部provider条件。

**系统验收状态：PARTIAL。** 声明单机拓扑的离线闭环、持久restart、可靠receipt恢复和E5容量/生命周期断言已经通过；完整非integration suite仍失败，性能及AT表其他缺口未关闭。真实account+user verifier与compact契约分别BLOCKED。receipt之前没有可靠事实的UNKNOWN/ORPHANED保留占用、人工核对、不重放，是允许的安全结果，不是自动恢复结算。

**生产启用条件：** 先关闭两个内部旁路竞态、完成剩余I类验收与失败归因/处置、满足原20ms事务目标和可接受吞吐，再取得可验证的provider身份及真实compact契约。使用固定schema/key版本、受管执行者完整清单、单主库和单noeviction Redis；外部插件二进制与实际日志/APM采集另行审计。HA、跨地域双活、Redis Cluster不支持。条件满足前保持默认关闭，禁止伪造VERIFIED、删除未知占用或恢复旧token。

迁移261及逐主体安全回滚顺序见 [rollout.md](rollout.md)。本轮未部署，也不把“完成了运行”写成七阶段全部验收完成。
