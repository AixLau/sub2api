package promptfilter

// ATT&CK/OWASP vocabulary is evidence for the semantic reviewer, not proof of
// malicious intent. Keep every combination non-operational so quotes, negation
// and authorized testing are adjudicated with the complete outer task.
const cyberRequestActionEN = `\b(?:use|run|execute|perform|conduct|implement|build|create|write|generate|deploy|install|exploit|weaponize|weaponise|help)\b`
const cyberRequestActionZH = `(?:使用|运行|执行|实施|构造|编写|生成|开发|部署|植入|利用|武器化|帮我)`
const cyberExploitTechnique = `(?:\b(?:exploit(?:s|ation)?|payloads?|poc|proof[-\s]?of[-\s]?concept|r[.\s_-]*c[.\s_-]*e|sqli|ssrf|ssti|xxe|deserialization|jndi|log4shell)\b|漏洞利用|利用链|攻击载荷|命令注入|反序列化|远程代码执行)`
const cyberExecutionOutcome = `(?:\b(?:(?:execute|run)\s+(?:(?:arbitrary|remote|system|shell)\s+)?(?:commands?|code)|(?:get|gain|obtain|spawn)\s+(?:a\s+)?(?:root\s+|remote\s+)?shell|(?:deploy|install|upload)\s+(?:a\s+)?(?:web\s*shell|backdoor)|exfiltrate|steal)\b|执行.{0,12}(?:命令|代码)|运行.{0,12}(?:命令|代码)|拿到.{0,8}shell|获取.{0,8}shell|写入.{0,8}(?:木马|后门)|上传.{0,8}(?:木马|webshell)|窃取|外传)`

var cyberIntentPatternConfigs = []PatternConfig{
	{
		Name: "candidate_ctf_artifact_context",
		Regex: `(?i)\b(?:ctf|capture\s+the\s+flag|crackme|pwn\s+challenge|web\s+challenge|reverse\s+challenge|crypto\s+challenge|forensics?\s+challenge|misc\s+challenge)\b|(?:Web|Pwn|Reverse|Crypto|Forensics|Misc)\s*题|` +
			`(?:CTF|夺旗|靶场|题解|writeup|flag|比赛题目).{0,100}(?:ELF|PE\s*文件|APK|PCAP|内存镜像|Ghidra|IDA|pwntools|libc|ROP|SSTI|SSRF|SQLi|RSA|ECDSA|Volatility|Docker|nc|netcat|题目附件|挑战实例)|` +
			`(?:题目附件|题目提供|题目).{0,100}(?:ELF|PE\s*文件|APK|PCAP|内存镜像|Ghidra|IDA|pwntools|libc|ROP|SSTI|SSRF|SQLi|RSA|ECDSA|Volatility|Docker|nc|netcat)|` +
			`(?:ELF|PCAP|APK|内存镜像|题目附件|挑战实例).{0,100}(?:flag|writeup|题目|challenge|靶场|CTF)`,
		Weight: 15, Category: "ctf",
	},
	{
		Name: "candidate_exploit_execution",
		Regex: `(?i)` + cyberExploitTechnique + `.{0,160}` + cyberExecutionOutcome + `|` +
			cyberExecutionOutcome + `.{0,160}` + cyberExploitTechnique,
		Weight: 80, Category: "exploit",
	},
	{
		Name: "candidate_security_control_bypass",
		Regex: `(?i)\b(?:bypass|circumvent|evade|defeat|disable|patch\s+out|unhook)\b.{0,100}\b(?:waf|rasp|edr|amsi|etw|uac|mfa|2fa|anti[-\s]?virus|antivirus|captcha|authentication|authorization|signature\s+(?:check|verification)|certificate\s+pinning)\b|` +
			`(?:绕过|规避|突破|关闭|禁用|卸载钩子|去除).{0,60}(?:WAF|RASP|EDR|AMSI|ETW|UAC|MFA|2FA|验证码|多因素认证|双因素认证|身份认证|身份验证|权限校验|签名校验|证书绑定|安全检测|杀软)|` +
			cyberRequestActionZH + `.{0,50}(?:免杀|白加黑|杀软绕过)|` + cyberRequestActionEN + `.{0,60}\b(?:av[-\s]?evasion|edr[-\s]?evasion)\b`,
		Weight: 75, Category: "evasion",
	},
	{
		Name: "candidate_privilege_escalation_operation",
		Regex: `(?i)\b(?:exploit|abuse|escalate|elevate)\b.{0,100}\b(?:sudo|suid|setuid|capabilities|seimpersonate(?:privilege)?|token\s+impersonation|kernel|uac|privesc|privilege\s+escalation)\b.{0,100}\b(?:root|system|administrator|admin|shell)\b|` +
			`\b(?:privesc|privilege\s+escalation|lpe|uac\s+bypass)\b.{0,100}\b(?:gain|obtain|get|spawn|execute|run)\b.{0,60}\b(?:root|system|administrator|admin|shell|commands?)\b|` +
			`(?:利用|滥用).{0,80}(?:SUID|sudo|令牌模拟|内核漏洞|SeImpersonate|UAC).{0,80}(?:提权|root|SYSTEM|管理员权限)|` +
			`(?:提权|权限提升|本地提权).{0,60}(?:获取|拿到|获得|执行|运行).{0,40}(?:root|SYSTEM|管理员|shell|命令)|(?:获取|拿到|获得).{0,30}(?:root|SYSTEM|管理员权限).{0,60}(?:提权|权限提升)`,
		Weight: 80, Category: "post_exploitation",
	},
	{
		Name: "candidate_lateral_movement_operation",
		Regex: `(?i)\b(?:lateral\s+movement|pivot(?:ing)?|pass[-\s]?the[-\s]?(?:hash|ticket)|pth|ptt|ntlm\s+relay)\b.{0,120}\b(?:execute|run|compromise|take\s+over|access|authenticate|spread)\b.{0,80}\b(?:hosts?|servers?|domain\s+controllers?|accounts?|networks?|machines?)\b|` +
			`\b(?:lateral\s+movement|pivot(?:ing)?|pass[-\s]?the[-\s]?(?:hash|ticket)|pth|ptt)\b.{0,100}\b(?:hosts?|servers?|domain\s+controllers?|machines?)\b.{0,100}\b(?:execute|run|compromise|take\s+over|access|authenticate)\b|` +
			cyberRequestActionEN + `.{0,80}\b(?:psexec|wmiexec|smbexec|winrm|evil[-\s]?winrm|pass[-\s]?the[-\s]?(?:hash|ticket)|pth|ptt)\b.{0,100}\b(?:stolen|compromised|unauthori[sz]ed|victim)\b|` +
			`(?:横向移动|横移|哈希传递|票据传递|NTLM\s*中继|内网跳板).{0,100}(?:执行|接管|控制|访问|窃取|登录|扩散).{0,60}(?:主机|服务器|域控|账号|内网|凭证)|` +
			`(?:横向移动|横移|哈希传递|票据传递).{0,100}(?:主机|服务器|域控|账号|内网).{0,60}(?:执行|接管|控制|访问|窃取|登录)|` +
			`(?:使用|利用|执行).{0,60}(?:PsExec|WMIExec|SMBExec|WinRM|PtH|PtT).{0,100}(?:被盗|失陷|未授权|受害者|横向移动|横移)`,
		Weight: 80, Category: "post_exploitation",
	},
	{
		Name: "candidate_persistence_operation",
		Regex: `(?i)\b(?:install|create|add|register|modify|abuse)\b.{0,100}\b(?:scheduled\s+tasks?|schtasks|cron(?:tab)?|systemd|registry\s+run\s+keys?|startup\s+(?:items?|entries)|wmi\s+(?:events?|subscriptions?)|launch\s+agents?)\b.{0,120}\b(?:backdoor|covert|hidden|malicious|c2|command[-\s]and[-\s]control|persistent\s+access)\b|` +
			`\b(?:establish|maintain|deploy|install)\b.{0,80}\b(?:covert\s+persistence|persistent\s+backdoors?|bootkits?|rootkits?)\b|` +
			`(?:创建|添加|修改|注册|滥用).{0,80}(?:计划任务|定时任务|注册表启动项|启动项|WMI事件|cron|systemd).{0,100}(?:后门|隐蔽|恶意|C2|远控|持久化控制)|` +
			`(?:建立|保持|部署|植入).{0,60}(?:隐蔽持久化|持久化后门|开机后门|rootkit|bootkit)`,
		Weight: 80, Category: "post_exploitation",
	},
	{
		Name: "candidate_domain_credential_operation",
		Regex: `(?i)\b(?:dump|extract|steal|harvest|forge)\b.{0,100}\b(?:lsass|sam\s+(?:hashes|database)|ntds[.\s_-]*dit|krbtgt|kerberos\s+tickets?|ntlm\s+hashes?|golden\s+tickets?|silver\s+tickets?)\b|` +
			cyberRequestActionEN + `.{0,100}\b(?:dcsync|dcshadow|kerberoast(?:ing)?|as[-\s]?rep[-\s]?roast(?:ing)?|secretsdump|lsadump|password\s+spraying|credential\s+stuffing)\b|` +
			`(?:转储|提取|窃取|伪造).{0,80}(?:LSASS|NTDS|krbtgt|NTLM哈希|Kerberos票据|黄金票据|白银票据|域控凭证)|` +
			cyberRequestActionZH + `.{0,60}(?:DCSync|DCShadow|Kerberoasting|AS[-\s]?REP|密码喷洒|撞库|黄金票据攻击|白银票据攻击)`,
		Weight: 80, Category: "credential_attack",
	},
	{
		Name: "candidate_process_injection_operation",
		Regex: `(?i)` + cyberRequestActionEN + `.{0,100}\b(?:process\s+hollowing|dll\s+(?:injection|side[-\s]?loading)|reflective\s+(?:dll\s+)?loading|apc\s+injection|thread\s+hijacking|shellcode|hooks?)\b.{0,120}\b(?:evade|conceal|steal|backdoor|bypass\s+(?:edr|authentication|login))\b|` +
			cyberRequestActionZH + `.{0,80}(?:进程镂空|进程空洞|进程注入|DLL\s*(?:注入|侧载)|反射加载|APC\s*注入|线程劫持|shellcode|hook|钩子).{0,100}(?:免杀|隐藏恶意|窃取|植入后门|绕过\s*(?:EDR|登录|认证))`,
		Weight: 80, Category: "evasion",
	},
	{
		Name: "candidate_intrusion_coverup",
		Regex: `(?i)\b(?:clear|delete|wipe|tamper\s+with|disable)\b.{0,80}\b(?:event\s+logs?|audit\s+logs?|logging|security\s+logs?)\b.{0,100}\b(?:hide|conceal|cover)\b.{0,60}\b(?:intrusion|attack|tracks|unauthori[sz]ed\s+access)\b|` +
			cyberRequestActionEN + `.{0,80}\b(?:timestomp(?:ing)?|anti[-\s]?forensics)\b.{0,100}\b(?:hide|conceal|cover|evade)\b|` +
			`(?:清除|删除|擦除|篡改|关闭).{0,60}(?:事件日志|审计日志|安全日志|日志记录).{0,80}(?:隐藏|掩盖|消除).{0,40}(?:入侵|攻击|痕迹|未授权访问)|` +
			`(?:伪造时间戳|时间戳篡改|反取证).{0,80}(?:隐藏|掩盖|逃避).{0,40}(?:入侵|攻击|调查|检测)`,
		Weight: 80, Category: "evasion",
	},
}
