# 多凭证系统验收账本

验收起点与八个提交见 [baseline](credential-acceptance/baseline.md)。本表覆盖 AT-01～AT-40，每行独立结论。

状态口径：PASS=指定拓扑中完整场景有实际证据；PARTIAL=组件或部分系统场景通过，仍有缺口/失败；BLOCKED=缺外部契约或执行前提，不能绕过；NOT_RUN=本轮尚未运行。测试函数名称不代表整项通过。

本轮初始审计：旧记录仅有组件/部分链路证据，全部暂列 PARTIAL；真实 provider/compact 为 BLOCKED。后续按实际结果更新，不沿用“代表性复现证明所有基线失败”的结论。

| AT | 状态 | 场景及必须结果 | 证据/实际命令 | 缺口 |
|---|---|---|---|---|
| AT-01 | BLOCKED | 同主体导入三次独立授权；三实例、一主体，独立版本 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 真实provider身份/compact契约缺失；仅mock可测 |
| AT-02 | PARTIAL | 同 token / 已知同 refresh family 重复导入；拒绝重复实例，不建立第二刷新器 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-03 | PARTIAL | 同工作区不同用户导入；不自动合并个人主体 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-04 | PARTIAL | 导入验证部分失败/提交重放；不部分激活、不重复创建 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-05 | PARTIAL | token refresh；installation/profile 不变 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-06 | PARTIAL | 进程重启、普通配置更新；实例身份和活跃绑定不变 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-07 | PARTIAL | 替换授权；新世代；旧绑定不使用新凭证 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-08 | PARTIAL | 三节点同时抢最后一个总槽位；仅一条新 lease 获批 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-09 | PARTIAL | C=10、实例上限均为8；允许8/2/0，不允许合计11 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-10 | PARTIAL | C=12、权重1:2:1、需求10:2:10；可达到5:2:5且不保留空槽 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-11 | PARTIAL | C=0或实例hard_max=0；不准入相应工作；不解释为无限 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-12 | PARTIAL | C从10降5、已有8占用；不杀请求；明确overhang；禁止新准入 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-13 | PARTIAL | 扩容/降实例健康容量；新准入遵循新版本，旧请求非抢占 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-14 | PARTIAL | 满绑定实例与空闲其他实例；老会话等待，新会话可用其他实例 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-15 | PARTIAL | 同新session多节点并发；只有一个有效实例绑定 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-16 | PARTIAL | 不同用户相同session字符串；不串绑定、权限或结果 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-17 | PARTIAL | TTL到期但存在活跃lease；绑定不被驱逐 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-18 | PARTIAL | 过期旧状态恢复；明确拒绝/显式迁移，不静默换实例 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-19 | PARTIAL | 无session但有状态续接；不随机路由，返回兼容错误 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-20 | BLOCKED | 普通、透传、compact；均受同一总额和选中凭证约束 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 真实provider身份/compact契约缺失；仅mock可测 |
| AT-21 | PARTIAL | 分组账号进入WS/不支持路径；不落入绕过总额的旧路径 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-22 | PARTIAL | 队列等待和队头实例冷却；不占执行槽，其他就绪请求可前进 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-23 | PARTIAL | 入队与取消竞态；只出现取消或有效执行之一，无幽灵票据 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-24 | PARTIAL | acquire提交响应丢失；同ID查询恢复，不重复批准 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-25 | PARTIAL | 双release/迟到旧owner释放；计数不负、不影响新lease | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-26 | PARTIAL | 运行时节点失联/心跳过期；ORPHANED仍计占用，发出告警 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-27 | PARTIAL | 流式首token/半途断流；首token不释放；未知结果不重放 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-28 | PARTIAL | 旧token 401到达新版本之后；不停用新版本凭证 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-29 | PARTIAL | 并发refresh/远端成功本地失败；单刷新族互斥；未知结果停止盲重试 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-30 | PARTIAL | 429含Retry-After/共享额度耗尽；等待不截短，不换凭证规避 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-31 | PARTIAL | 同幂等键同内容/异内容；不重复执行；异内容409 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-32 | PARTIAL | usage/outbox重复消费；不重复本地结算；未知非零化 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-33 | PARTIAL | Redis缓存全部丢失；软状态重建；总并发不失守 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-34 | PARTIAL | 数据库不可达/切换/旧节点恢复；fail closed；不旁路新dispatch | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-35 | PARTIAL | 配置通知丢失/并发修改；准入读权威；旧If-Match冲突 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-36 | PARTIAL | secret日志扫描/越权管理；不泄漏token；tenant边界有效 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-37 | BLOCKED | 旧单账号迁移；保留凭证、seed、组权限和计费配置 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 真实provider身份/compact契约缺失；仅mock可测 |
| AT-38 | PARTIAL | drain及回滚；旧绑定可追踪，不留下混用计数 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-39 | PARTIAL | 大整数/未知JSON/map类型/重复header；非目标字段保真；歧义有确定处理 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |
| AT-40 | PARTIAL | 直接调用旧Account API/任务入口；不能绕过新总额或改写受控实例字段 | [旧阶段记录](multi-credential-implementation.md)，本轮待复核 | 尚无本轮完整系统证据 |

代码实现、系统验收和生产启用是三个不同结论。当前系统验收未完成，生产禁止启用。
