# 本地准入隔离与有界公平

`report_revision=local-admission-fairness-r1`，工作起点 `44f88b3d55c170205b1e6d120895f8691132bb3a`。用户最后限定为**仅本机测试验收**：原生Linux/amd64复测不再作为本轮执行项。本机Darwin/arm64、OrbStack/aarch64、PostgreSQL镜像linux/amd64且实际18.4；不能称为原生或独占硬件SLO。生产主机仅做过只读架构、负载检查，无测试部署、服务中断、配置变更或生产数据访问。

20ms严格指规格21.2的**权威admission事务p95**。本地调度等待、跨节点try-lock竞争、call/请求到获准时间另列，并对实际handler使用15秒预算验收。E3已通过的生命周期保护、可靠receipt恢复是回归约束；不再统称“生命周期饥饿仍未解决”。没有放宽上限、延长期限或删除权威检查。

## 静态发现、复现和修复

| 问题 | 基线实际复现 | 实现与证据 |
|---|---|---|
| A跨节点锁竞争占住整个store的本地gate，阻塞B | A已实际rollback至少两次、其外部锁保持时，B的750ms期限耗尽 | `ec091df81`按主体保留本地队列，跨节点失败后让出节点turn再重试；B约315.58ms获准；节点仍最多一个准入/推进事务。详见 [主体隔离](local-principal-isolation.md) |
| offered/pump无限优先 | 第9个高优先仍取得grant，fresh无机会 | `4c0c3f8fe`：每类FIFO，队头fresh最多被8个已完成的高优先turn越过。界限按turn计，不承诺被外部数据库阻塞时的墙钟时限 |
| 已取消等待者仍占256项本地名额 | 256个acquire均返回取消，切片仍满；新请求误报STORE_UNAVAILABLE | 同提交及时移除取消者；grant/cancel在同mutex下只转交一次；新`ErrAdmissionLocalQueueFull`映射独立REJECTED/503 |
| handler确认晚于15秒后发送／未登记超时误报存储错误 | 实际mock收到late ACK/lost ACK请求；未登记3请求到期仍报STORE_UNAVAILABLE | `b53f2b7d8`返回或恢复同RESERVED后重查期限，以原3秒补偿预算撤销；见 [handler 15秒证据](handler-queue-budget.md) |

registry对所有主体合计最多257个有效调用，含一个运行者和256个等待者；不会将等待容量按主体倍增。清理最后引用后回收主体条目；取消context可立即返还计数，但不提前拆分仍被旧调用引用的主体队列。queue pump按主体轮流取得节点turn，单次失败不阻断其他主体。没有schema迁移、连接预算或业务功能增加。

## 已运行定向检查

所有Go命令在`backend/`，使用`GOCACHE=/tmp/sub2api-queue-progress/go-cache`；integration另设`TESTCONTAINERS_RYUK_DISABLED=true CI=true`，均保持vet开启。

- gate基线3项FAIL；修复`go test -p 2 ./internal/repository -run '^TestCredentialAdmissionTurn' -count=1 -timeout=60s -v` PASS；`-race -count=10` PASS，含1000次grant/cancel交错。原始日志 `/tmp/sub2api-local-admission-fairness/{gate-baseline,gate-fixed,gate-race}.log`。
- 主体隔离基线FAIL；修复`go test -p 2 -tags=integration ./internal/repository -run '^(TestCredentialPrincipalTurn|TestCredentialQueueProgress|TestCredentialLifecycleResources|TestPrincipalAdmissionThreeNodesLastSlot)' -count=1 -v` PASS；registry单测和race各20轮PASS。日志 `/tmp/sub2api-local-admission-isolation/`。
- handler 15秒实际突发、取消、late ACK及本地满错误码集成PASS（76.194秒）；本地满通过wrapper注入decision验证HTTP映射，不冒称该用例触发了真实256限额。日志 `/tmp/sub2api-local-admission/handler/after-stable.log`。
- 计时器自身增加真实PG验证`TestCredentialAdmissionTraceCountsRetriesAndCommit`：一个advisory失败回滚、一个成功提交，分别计数且累计事务时间包含两次尝试；PASS（8.316秒），`/tmp/sub2api-local-admission/root/trace-verify.log`。

## 单主体成本测量

本轮保留原C+3 worker、3节点pool各6+2总24连接、2分钟request/context、100ms批量提示轮询、每分钟C+3 barrier、原200ms/2s/5s/12s mock分布、Heartbeat10s/3s及Finish5s。只更新测量：

- 每次try-lock实际布尔结果（成功/失败）计数，不用SQL文本出现次数推断获锁。
- 最后一次权威事务时长与全部重试事务累计时长分开；每次重新Begin清除上次锁持有起点，记录成功/回滚/错误计数。
- 主体锁取得后到事务结束的平均持有时间下界、事务平均时间、有效成功准入/秒、成功准入/try-lock尝试比率。
- 所有事务的平均时长单列，**其中包含短失败重试、Heartbeat、Finish等，不能冒称成功准入事务均值**。
- 未带请求trace的后台SQL累计wall时间/次数（queue/control）单列，线程重叠时不能将和解释成墙钟CPU占比。
- 本地待调用数读取主体registry，不能只读节点gate漏掉等主体turn的请求。采样估计并非准确空闲墙钟，更不能外推真实权限/健康变化时所有请求可执行。

30秒C200/I16诊断命令：

```sh
GOCACHE=/tmp/sub2api-queue-progress/go-cache TESTCONTAINERS_RYUK_DISABLED=true CI=true \
SUB2API_CREDENTIAL_ENDURANCE=30s SUB2API_CREDENTIAL_MATRIX_CASE=C200_I16 \
go test -p 2 -tags=integration ./internal/repository -run '^TestCredentialAcceptanceEndurance$' -count=1 -timeout=4m -v
```

实际PASS；1647次dispatch、37.14/s，无ERROR，最终全部RELEASED。成功准入事务平均19.79ms、p95 28.49ms，主体持锁下界平均9.255ms；call p95 5601.61ms，前置等待p95 5578.30ms。共有12241次advisory尝试，1647次获锁、10594次失败回滚，有效准入/尝试13.45%；没有WAIT或后台推进样本，不能据此判断有竞争票据时的成本。它显示单主体仍有大量跨节点无效重试，隔离修复没有天然提高单主体吞吐。没有用p95倒数推断上限。日志`/tmp/sub2api-local-admission/root/single-principal-cost.log`。

## 本机固定矩阵 E4

实际命令（backend）：

```sh
GOCACHE=/tmp/sub2api-queue-progress/go-cache TESTCONTAINERS_RYUK_DISABLED=true CI=true \
SUB2API_CREDENTIAL_ENDURANCE=10m go test -p 2 -tags=integration ./internal/repository \
-run '^TestCredentialAcceptanceEndurance$' -count=1 -timeout=85m -v
```

`tested_code_sha=benchmark_code_sha=d2ee90c3c1d634f7fc9956ae595de858bbcb85ff`，原二进制SHA-256 `855af0cee9eef0b881054bac8cafe401e5f01d136f885f9835e636116d32e740`。六组串行各10分钟，**命令退出1**。原始日志`/tmp/sub2api-local-admission/root/endurance-E4-local.log`，机器结果[local-admission-E4-results.json](local-admission-E4-results.json)。全部结果为本机跨架构、非独占环境证据。

| 场景 | 执行断言 | 成功事务均值 / p95 ms | 主体持锁均值下界 ms | call p95 ms | 有效准入/s | try-lock失败 / 总尝试 | 后台SQL数 |
|---|---|---:|---:|---:|---:|---:|---:|
| C10/I3 | PASS | 22.15 / 37.60 | 17.73 | 98.95 | 9.00 | 20646 / 45471 | 94953 |
| C10/I16 | FAIL | 836.68 / 3098.73 | 478.64 | 39471.05 | 0.50 | 31710 / 33384 | 12359 |
| C50/I3 | PASS | 41.59 / 107.42 | 20.28 | 5236.55 | 20.08 | 159997 / 178411 | 19087 |
| C50/I16 | PASS | 27.27 / 46.18 | 17.43 | 970.44 | 20.46 | 180645 / 207269 | 52140 |
| C200/I3 | PASS | 23.74 / 39.72 | 10.88 | 8213.20 | 41.36 | 184723 / 210150 | 0 |
| C200/I16 | PASS | 24.39 / 40.17 | 11.29 | 8509.57 | 40.38 | 185348 / 210276 | 0 |

try-lock全局计数含准入及queue pump的实际返回值；有效准入/尝试比另按全部`admit.*`结果统计（包括ERROR）。报告保留原始比率，并从`operation_counts`重算，修复后发生器也已覆盖所有结果。此显示修正不改变受测代码执行路径，未为此重跑整小时。所有事务的均值包含短失败回滚，不能当成功准入均值。

C10/I16记录9次Finish错误、14次admit错误、125次明确队列超时，末尾9条DISPATCHING继续占用；没有清零。其他五组无ERROR且全部lease RELEASED。主机在C10/I3尾段、C10/I16期间同时执行另一工作流的Go编译，本机load约63.66、swap使用约9174MiB，测试PG采样CPU约425%。主机之后负载回落，与吞吐恢复同时发生。保留`environment/contention.json`及只读DB快照；这能证明资源争用同时存在，**不能独立归因每一条失败，也不能把整轮标为PASS**。

C200/I3、I16实际mock容量利用率分别19.82%、19.35%，仍有容量且本地应用队列几乎全程有等待。获准事务均值23.74/24.39ms，后台提示SQL为0（没有WAIT分支），而try-lock失败184723/185348次；该场景每次成功获准伴随约7.3/7.4次失败尝试。它支持“单主体串行准入与跨节点重试仍限制吞吐”的结论。各事务/生命周期可能时间重叠，未用p95倒数或不同操作均值相加推算精确极限，也未声称隔离修复提高单主体处理率。

C50两组除准入竞争外仍有批量推进成本：后台SQL累计wall约17.19s/60.87s，分别19087/52140次；该累计包含并发，不能直接视为占用比例。C200的Finish p95为57.27/51.40ms，Heartbeat p95为70.59/55.87ms，均无错误；E4中的C10资源争用失败说明不能将E3预算通过推广到任意主机资源失常情形。

首次矩阵失败的C10/I16另以**同一保存二进制**复核，保持10分钟、全部deadline及生命周期预算，不重新编译工作树：PASS，5537次dispatch全部RELEASED，无ERROR/明确队列超时。成功事务均值23.62ms、p95 43.70ms；call p95 106.67ms；Finish最大319.85ms、Heartbeat最大127.85ms。结果见[同二进制复核](local-admission-E4-C10-I16-repeat.json)。这只证明该失败未在同一代码的后续运行持续复现，不独立证明每条首次失败的因果归属；首次六组合命令退出1的事实保留。

复核命令：`TESTCONTAINERS_RYUK_DISABLED=true CI=true SUB2API_CREDENTIAL_ENDURANCE=10m SUB2API_CREDENTIAL_MATRIX_CASE=C10_I16 /tmp/sub2api-local-admission/root/matrix-tested.test -test.run='^TestCredentialAcceptanceEndurance$' -test.count=1 -test.timeout=15m -test.v`，退出0；日志`E4-C10-I16-repeat.log`。

矩阵运行期间另一个工作流提交了`f416f1a93`（public transit/channel monitor），该变更保留，但不属于E4二进制或本专项实现。最终HEAD不能代替受测SHA；新指标展示修正（包含ERROR尝试、把准入本地等候超时标为pre_transaction_wait_failed）也不影响已保存二进制的产品路径。只读复核、数据重算及文档修订不触发重复完整矩阵。

## 迁移、回滚和未验证

本轮无DB迁移；保留既有259 offer列及生命周期保留池、receipt恢复协议。回滚前保持功能关闭，暂停grouped新请求并按原证据完成已知终结工作；不清空账本、UNKNOWN或旧绑定，不恢复旧token快照。回退相应代码会重新引入已复现的隔离/饥饿或late ACK问题，因此不能作为生产启用方案。

原生Linux/amd64、独占宿主性能、三个完整网关进程的发布演练仍未验证；本轮按最后指令仅本机。真实provider身份与compact继续BLOCKED，导入UNVERIFIED；功能默认关闭。权限和未知执行边界未放宽，WS、session/full、UA/TLS、隐私清理及原未跟踪文件均未修改。


只读复核边界：两层本地队列各自使用有界优先；“8个高优先turn”是每个queue队头fresh的界限，不是TryAdmit全程最多等待8个turn，更不是墙钟保证。跨主体已复现/修复的是advisory try-lock失败后的应用层等待持有node gate。取得advisory之后，若user/principal行锁或INSERT发生真实数据库阻塞，仍会持有节点唯一事务预算；没有宣称任意数据库锁下的主体隔离。这一限制未扩展为更换锁协议或增加节点连接预算。


收尾度量检查：代码`225168175`在最终工作树上执行`GOCACHE=/tmp/sub2api-queue-progress/go-cache TESTCONTAINERS_RYUK_DISABLED=true CI=true SUB2API_CREDENTIAL_ENDURANCE=1s SUB2API_CREDENTIAL_MATRIX_CASE=C10_I3 go test -p 2 -tags=integration ./internal/repository -run '^TestCredentialAdmissionTraceCountsRetriesAndCommit$|^TestCredentialAcceptanceEndurance$|^TestCredentialGatewayQueueBudgetLocalOverload' -count=1 -timeout=3m -v`，退出0（16.330秒），`final-metrics-check.log`。包括计时计数自校验、本地过载错误码/已有票据补偿及1秒发生器冒烟；该短样本不进入性能对照，也不等于再次完整矩阵通过。
