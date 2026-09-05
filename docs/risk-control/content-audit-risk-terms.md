# 内容审核：网络安全术语与意图规则

## 判定原则

词库负责发现需要审核的请求，语义模型负责结合外层任务、操作动作、目标、授权范围和实际结果作出判断。当前 `candidate_only` 模式中，新增组合规则不直接产生违规结论。初审可返回内部 `review`，二审只能返回 `allow` 或 `reject`；技术故障单独处理。

正常代码分析、授权逆向分析、安全研究、漏洞修复、入侵检测和防御性讨论不能仅因出现技术词被拒绝。引用、否定句和日志中的攻击描述必须按外层任务判断；同一请求中明确要求执行有害操作时，不能被前面的“仅研究”“仅防御”声明覆盖。

第三方软件授权破解、未授权入侵、凭证窃取、恶意持久化等仍属于拒绝对象。平台的病毒学意图限制独立存在，不由本次网络安全术语调整改变。

## 术语分类

“高风险组合”指较明确的攻击动作与目标或结果组合，仍需区分真实请求和被分析的材料。右列通用术语不构成单词黑名单；未加入表达式的别名仅作为语义审核与维护词库时的参考。

| 场景 | 高风险组合方向 | 同时用于研究、开发或防御的术语 |
| --- | --- | --- |
| 逆向与运行分析 | 破解/绕过 + 第三方授权校验、激活、付费功能；Hook + 窃取凭证/绕过认证 | 逆向、反编译、反汇编、脱壳、解包、调试、补丁、抓包；reverse engineering、binary analysis、decompile/decompilation、disassemble/disassembly、unpack、patch、Hook、instrumentation；IDA、Ghidra、Frida、JADX、dnSpy、ILSpy |
| 安全评估 | 攻击/接管 + 第三方服务器或账号；使用被盗凭证 + 远程执行工具 | 渗透测试、红队、蓝队、攻防演练、漏洞扫描、资产探测；pentest、penetration testing、red team、CTF、VAPT；Nmap、Burp、Nuclei、ProjectDiscovery HTTPX |
| 漏洞利用 | 漏洞/利用链 + 执行命令、获取 shell、植入后门、窃取或外传 | 漏洞利用、注入、载荷、概念验证；Exploit、Payload、PoC、RCE、SQLi、SSRF、SSTI、XXE、JNDI、反序列化、ROP、UAF |
| 安全机制绕过 | bypass/disable/unhook + 认证、签名校验、检测；构造 + 免杀或白加黑 | WAF、RASP、EDR、AMSI、ETW、UAC、MFA、2FA、验证码、certificate pinning；绕过本地缓存或修改普通设置不是同一种操作 |
| 权限提升 | 利用权限机制 + 获取 root/SYSTEM；提权 + 执行管理员命令 | 提权、权限提升、privesc、privilege escalation、LPE、SUID、setuid、sudo、SeImpersonatePrivilege、token impersonation、内核漏洞 |
| 横向移动 | PtH/PtT/横移 + 访问或控制其他主机；使用被盗凭证 + PsExec/WMIExec/WinRM | lateral movement、pivot、pass-the-hash、PtH、pass-the-ticket、PtT、NTLM relay、哈希传递、票据传递、内网跳板 |
| 持久化 | 创建任务或启动项 + 后门/隐蔽远控；部署 + 持久化后门 | persistence、权限维持、scheduled task、schtasks、cron、systemd、WMI subscription、registry Run key、launch agent、rootkit、bootkit |
| 域与凭证攻击 | 运行/实施 + DCSync、Kerberoasting、密码喷洒；转储/伪造 + 凭证或票据 | LSASS、SAM、NTDS.dit、krbtgt、Kerberos、NTLM、credential dumping、DCSync、DCShadow、AS-REP roasting、secretsdump、lsadump、黄金票据、白银票据 |
| 注入与隐藏 | 构造/使用 + 注入技术 + 隐藏恶意代码、窃取或绕过检测 | process injection、DLL injection、DLL side-loading、process hollowing、APC injection、reflective loading、thread hijacking、shellcode、进程镂空、线程劫持 |
| 入侵痕迹 | 删除/篡改审计日志 + 掩盖入侵；伪造时间戳 + 逃避调查 | anti-forensics、timestomping、event logs、日志轮转、取证分析；常规保留策略清理不是掩盖入侵 |

## 新增组合配置

配置位于 `backend/internal/pkg/promptfilter/builtin_cyber_intent_patterns.go`，经 `candidatePatternConfigs` 加载，使用现有规则版本和候选审核机制。

| 规则标识 | 必须出现的组合证据 |
| --- | --- |
| `candidate_exploit_execution` | 利用技术 + 执行命令/获取 shell/后门/窃取，支持动作与技术交换顺序 |
| `candidate_security_control_bypass` | 绕过或禁用动作 + 具体安全机制，或操作动作 + 免杀语义 |
| `candidate_privilege_escalation_operation` | 提权技术 + 提升权限的实际操作或结果 |
| `candidate_lateral_movement_operation` | 横向技术 + 操作 + 主机目标，或操作工具 + 被盗/未授权条件 |
| `candidate_persistence_operation` | 修改启动/任务机制 + 恶意目的，或部署隐蔽持久化后门 |
| `candidate_domain_credential_operation` | 域攻击操作 + 技术，或窃取/转储/伪造 + 凭证对象 |
| `candidate_process_injection_operation` | 操作动作 + 注入技术 + 窃取/隐藏/后门等结果 |
| `candidate_intrusion_coverup` | 操作日志/时间戳 + 隐藏入侵或逃避调查的目的 |

已有 `candidate_unauthorized_access` 同时补充“攻击/接管第三方目标”的组合。

英文规则处理大小写、常见连接符和缩写，部分规则支持 `R.C.E`、`R_C_E`、`pass-the-hash` 等变体。全角、零宽字符处理复用现有规范化和扫描通道；不新增另一套字符串清洗器。距离限制用于关联动作和对象，不等同于语义证明。

## 过度匹配修正

- `Payload` 单独出现、JSON 字段和 agent 消息头不再命中 `generic_exploit`；攻击性修饰词或明确执行意图仍召回。
- 普通调试、补丁、静态分析不再单独命中逆向工具链；实际逆向术语、具体工具和二进制上下文仍可进入模型审核。
- `unpack` 需要二进制/可执行文件语境，普通归档解包不作为逆向候选。
- `PIE`、`NX`、`canary` 需要二进制保护语境；普通格式化字符串不再等同于格式化字符串漏洞。
- 普通人名 `John` 不再命中密码工具；`John the Ripper` 继续召回。
- Frida Hook 仅追踪认证函数不再独立触发 `frida_hook_abuse`；具体绕过或破解认证/授权的组合保留。
- `credential_dumping` 从可直接执行拦截的标记改为候选；实际窃取、转储和域攻击组合继续进入模型审核。
- 初审与终审去掉仅凭 `reverse_engineering` 类别强制拒绝的旧逻辑；模型 prompt 明确允许没有有害结果的授权逆向分析。

规则版本和模型指令版本均需更新，以隔离旧审核缓存。关键词配置作为代码发布，不要求修改管理员数据库配置。

## 验证

回归覆盖组合命中、中英文和缩写变体、普通技术讨论、开发和防御语境、否定/引用材料、包含有害操作的混合请求、模型认可的正常逆向分析，以及明确攻击和授权破解仍被拒绝。测试使用模拟审核模型；生产模型实际准确率需依赖部署后的带标注评估。

近期审核样本仅用于本地统计和片段回放。生产记录、原始请求和个人信息不写入本文件或测试夹具，回归样例使用独立构造的内容。

## 参考分类

- [MITRE ATT&CK：权限提升](https://attack.mitre.org/tactics/TA0004/)
- [MITRE ATT&CK：横向移动](https://attack.mitre.org/tactics/TA0008/)
- [MITRE ATT&CK：持久化](https://attack.mitre.org/tactics/TA0003/)
- [OWASP：攻击分类](https://owasp.org/www-community/attacks/)

这些资料用于归纳技术和攻击分类，不是“某术语出现即违规”的黑名单。审核策略由请求的实际意图及平台约束共同决定。
