package promptfilter

// Supplemental local rules distilled from public OWASP LLM01 and CTF Handbook
// taxonomies. Topic rules create a semantic-review candidate; operational rules
// are intentionally narrower and are eligible for the existing hard block.
// Sources: https://genai.owasp.org/llmrisk/llm01-prompt-injection/ and
// https://ctf101.org/.
const supplementalSourceRevision = "local-content-audit@2026-07-14"

var supplementalPatternConfigs = []PatternConfig{
	{
		Name:     "jailbreak_operational_request",
		Regex:    `(?i)\b(?:write|generate|create|give|provide|show|teach|help(?:\s+me)?|how(?:\s+to)?|prompt(?:\s+me)?)\b.{0,100}(?:jail\s*break|do\s+anything\s+now|developer\s+mode|unrestricted\s+mode|break\s+the\s+rules|bypass\s+(?:safety|content|moderation|guardrails?|policy)|ignore\s+(?:previous|system|developer)\s+instructions?)|(?:帮我|给我|生成|写出|提供|教我|如何).{0,100}(?:破限|破甲|越狱|解限|无限制模式|解除限制|绕过(?:安全|内容|审查|风控|审核|限制))`,
		Weight:   100,
		Category: "jailbreak",
		Strict:   true,
	},
	{
		Name:     "jailbreak_topic",
		Regex:    `(?i)\b(?:jail\s*break|do\s+anything\s+now|developer\s+mode|unrestricted\s+mode|direct\s+prompt\s+injection|indirect\s+prompt\s+injection|system\s+prompt\s+leakage|prompt\s+injection|jailbreak)\b|(?:越狱|破限|破甲|解限|无限制模式|提示注入|直接提示注入|间接提示注入|系统提示泄露|绕过安全策略)`,
		Weight:   25,
		Category: "jailbreak",
	},
	{
		Name:     "prompt_injection_override",
		Regex:    `(?i)(?:ignore|disregard|forget|override|bypass|skip|follow\s+only|do\s+not\s+follow|reveal|print|show|dump|extract).{0,80}(?:previous|system|developer|safety|hidden|above|earlier).{0,80}(?:instruction|prompt|message|rule|policy|content)|(?:忽略|无视|覆盖|绕过|跳过|泄露|输出|提取).{0,60}(?:之前|系统|开发者|安全|隐藏|上方|早先).{0,50}(?:提示|指令|规则|消息|政策|内容)`,
		Weight:   95,
		Category: "prompt_injection",
		Strict:   true,
	},
	{
		Name:     "system_prompt_extraction",
		Regex:    `(?i)(?:reveal|show|print|dump|quote|repeat|extract|leak|tell\s+me).{0,80}(?:system\s+prompt|developer\s+message|hidden\s+instructions?|initial\s+prompt|secret\s+rules?)|(?:泄露|显示|输出|打印|复述|提取|告诉我).{0,60}(?:系统提示|开发者消息|隐藏指令|初始提示|秘密规则)`,
		Weight:   95,
		Category: "prompt_injection",
		Strict:   true,
	},
	{
		Name:     "prompt_obfuscation_evasion",
		Regex:    `(?i)\b(?:payload\s+splitting|adversarial\s+suffix|indirect\s+prompt\s+injection|cross[-\s]?modal\s+injection|multilingual\s+attack|multi[-\s]?turn\s+jailbreak|base64|rot13|hex(?:adecimal)?|unicode|zero[-\s]?width|homoglyph|emoji|encode|decode)\b.{0,120}\b(?:prompt|jailbreak|injection|safety|filter|guardrail)\b|(?:载荷拆分|对抗后缀|间接提示注入|跨模态注入|多语言攻击|多轮越狱|base64|rot13|十六进制|unicode|零宽|同形异义|编码|解码).{0,80}(?:提示|注入|越狱|安全|过滤|护栏)`,
		Weight:   35,
		Category: "prompt_evasion",
	},
	{
		Name:     "agent_tool_permission_bypass",
		Regex:    `(?i)(?:call|invoke|use|execute|run|access|query|send|delete|upload|download).{0,80}(?:tool|function|plugin|browser|filesystem|api).{0,80}(?:without|ignore|bypass|skip|no).{0,40}(?:permission|approval|authorization|consent|restriction)|(?:绕过|跳过|无需|不需要).{0,50}(?:权限|审批|授权|确认).{0,50}(?:工具|函数|插件|浏览器|文件系统|API)`,
		Weight:   95,
		Category: "agent_abuse",
		Strict:   true,
	},
	{
		Name:     "ctf_security_challenge",
		Regex:    `(?i)\b(?:ctf|capture\s+the\s+flag|web\s+exploitation|binary\s+exploitation|reverse\s+engineering|penetration\s+testing|pentesting|red\s+team|exploit\s+development|bug\s+bounty|crackme|pwn(?:ing)?)\b|(?:CTF|夺旗|靶场|题解|渗透|渗透测试|逆向|逆向题|破解|破解题|PWN题)`,
		Weight:   10,
		Category: "ctf",
	},
	{
		Name:     "web_exploitation_technique",
		Regex:    `(?i)\b(?:sqli|sql\s+injection|union\s+select|blind\s+sqli|time[-\s]?based\s+sqli|command\s+injection|os\s+command\s+injection|shell\s+injection|rce|remote\s+code\s+execution|ssrf|server[-\s]?side\s+request\s+forgery|ssti|server[-\s]?side\s+template\s+injection|xxe|xml\s+external\s+entity|xss|cross[-\s]?site\s+scripting|csrf|request\s+smuggling|prototype\s+pollution|insecure\s+deserialization|idor|bola|jwt\s+(?:attack|bypass|forgery)|oauth\s+redirect|open\s+redirect|cors\s+misconfig(?:uration)?|path\s+traversal|directory\s+traversal|lfi|rfi|file\s+inclusion|http\s+parameter\s+pollution)\b|(?:SQL注入|命令注入|远程代码执行|服务端请求伪造|模板注入|XXE|反序列化|跨站脚本|跨站请求伪造|目录遍历|文件包含|原型链污染|越权|请求走私|参数污染|JWT攻击|OAuth重定向|开放重定向|CORS配置错误)`,
		Weight:   25,
		Category: "web_exploitation",
	},
	{
		Name:     "web_exploitation_operational_request",
		Regex:    `(?i)\b(?:write|generate|create|craft|construct|build|provide|give|show|use|exploit|bypass|attack|dump|retrieve|read|steal|get)\b.{0,100}(?:sql\s+injection|sqli|union\s+select|command\s+injection|rce|ssrf|ssti|xss|csrf|xxe|deserialization|path\s+traversal|directory\s+traversal|lfi|idor|bola|jwt|prototype\s+pollution|request\s+smuggling).{0,100}(?:payload|exploit|request|admin|root|shell|flag|secret|credential|file|database|cookie|token)|(?:利用|绕过|获取|读取|导出|窃取|写|生成|构造|制作|提供|给我).{0,100}(?:SQL注入|命令注入|RCE|SSRF|SSTI|XSS|CSRF|XXE|目录遍历|文件包含|越权|JWT|原型链污染|请求走私).{0,100}(?:载荷|利用|请求|管理员|后台|flag|机密|文件|数据库|cookie|token)`,
		Weight:   100,
		Category: "web_exploitation",
		Strict:   true,
	},
	{
		Name:     "web_payload_marker",
		Regex:    `(?i)(?:\bunion\s+select\b|\bor\s+1\s*=\s*1\b|\bsleep\s*\(|\bbenchmark\s*\(|(?:\.\./){2,}|%2e%2e|<script\b|javascript:|\{\{\s*\d+\s*[+*]\s*\d+\s*\}\}|169\.254\.169\.254|127\.0\.0\.1|localhost).{0,80}(?:payload|injection|exploit|bypass|flag|admin|secret|注入|利用|绕过|flag|机密|管理员)?`,
		Weight:   20,
		Category: "web_payload",
	},
	{
		Name:     "binary_exploitation_technique",
		Regex:    `(?i)\b(?:pwn|binary\s+exploitation|buffer\s+overflow|stack\s+overflow|heap\s+overflow|use[-\s]?after[-\s]?free|uaf|double\s+free|format\s+string|ret2libc|ret2csu|ret2dlresolve|rop|return[-\s]?oriented\s+programming|jop|srop|stack\s+pivot|shellcode|tcache\s+poisoning|fastbin|house\s+of\s+force|got\s+overwrite|plt\s*[/\\]\s*got|libc\s+leak|one[_\s-]?gadget|pwntools|pwning|canary|aslr|pie|nx|relro|seccomp)\b|(?:栈溢出|缓冲区溢出|堆溢出|格式化字符串|返回导向|栈迁移|释放后使用|双重释放|堆利用|GOT覆写|泄露libc|绕过canary|绕过ASLR|绕过NX|二进制利用|二进制漏洞|PWN题)`,
		Weight:   30,
		Category: "binary_exploitation",
	},
	{
		Name:     "binary_exploitation_operational_request",
		Regex:    `(?i)\b(?:write|generate|create|craft|construct|build|provide|give|show|use|exploit|bypass|control|hijack|get|obtain|利用|绕过|控制|劫持|获取|写|生成|构造|制作|提供|给我|使用)\b.{0,100}(?:buffer\s+overflow|stack\s+overflow|heap\s+overflow|use[-\s]?after[-\s]?free|uaf|double\s+free|format\s+string|ret2libc|ret2csu|ret2dlresolve|rop|return[-\s]?oriented|shellcode|tcache|got\s+overwrite|libc\s+leak|pwntools|canary|aslr|pie|nx|seccomp).{0,100}(?:payload|exploit|script|shell|root|control|bypass|flag)|(?:利用|绕过|控制|劫持|获取|写|生成|构造|制作|提供|给我|使用).{0,100}(?:栈溢出|缓冲区溢出|堆溢出|格式化字符串|返回导向|释放后使用|堆利用|PWN).{0,100}(?:载荷|利用|脚本|shell|提权|控制|绕过|flag)`,
		Weight:   100,
		Category: "binary_exploitation",
		Strict:   true,
	},
	{
		Name:     "ctf_crypto_technique",
		Regex:    `(?i)\b(?:single[-\s]?byte\s+xor|repeating[-\s]?key\s+xor|known[-\s]?plaintext|crib[-\s]?dragging|frequency\s+analysis|vigen(?:e|è)re|padding\s+oracles?|cbc\s+bit[-\s]?flipping|ecb\s+oracles?|length\s+extension|hash\s+collisions?|md5\s+collisions?|rsa[-\s]+(?:cryptography|encryption|cipher|keys?|signatures?|modulus|padding|attacks?|challenges?)|common\s+modulus|low[-\s]?exponent|wiener(?:'s)?\s+attack|hastad|discrete\s+log(?:arithm)?|weak\s+prng|nonce\s+reuse|ecdsa\s+nonce|hashcat|john(?:\s+the\s+ripper)?|rainbow\s+table|password\s+hash)\b|(?:单字节XOR|重复密钥XOR|已知明文|频率分析|填充预言机|CBC位翻转|ECB预言机|长度扩展|哈希碰撞|MD5碰撞|RSA(?:加密|密码|密钥|签名|模数|填充|攻击|题目)|共模|低指数|Wiener|Håstad|离散对数|弱随机数|nonce复用|ECDSA随机数|哈希破解|密码哈希|彩虹表)`,
		Weight:   25,
		Category: "crypto_attack",
	},
	{
		Name:     "crypto_key_recovery_request",
		Regex:    `(?i)(?:crack|break|decrypt|recover|derive|brute[-\s]?force|forge|solve|reverse|破解|解密|恢复|推导|爆破|伪造|求解|反推).{0,100}(?:key|password|hash|cipher|plaintext|secret|signature|nonce|xor|rsa|padding\s+oracle|md5|sha|ecdsa|密钥|密码|哈希|密文|明文|秘密|签名|随机数|XOR|RSA|填充预言机).{0,100}(?:challenge|ctf|flag|authentication|login|key|password|secret|题目|题解|flag|认证|登录|密钥|密码|机密)?`,
		Weight:   100,
		Category: "crypto_attack",
		Strict:   true,
	},
	{
		Name:     "reverse_engineering_toolchain",
		Regex:    `(?i)\b(?:crackme|reverse\s+engineering|binary\s+analysis|ida(?:\s+pro)?|ghidra|binary\s+ninja|radare2|x64dbg|ollydbg|gdb|frida|angr|z3|jadx|apktool|unpack(?:ing)?|decompil(?:e|er)|disassembl(?:e|y)|symbolic\s+execution|dynamic\s+instrumentation|anti[-\s]?debug|anti[-\s]?tamper|control[-\s]?flow\s+flattening|opaque\s+predicate|patch\s+(?:the\s+)?binary|nop\s+sled)\b|(?:逆向工程|静态分析|动态分析|反汇编|反编译|脱壳|调试|符号执行|动态插桩|反调试|反篡改|控制流平坦化|不透明谓词|补丁|破解题)`,
		Weight:   25,
		Category: "reverse_engineering",
	},
	{
		Name:     "reverse_engineering_operational_request",
		Regex:    `(?i)\b(?:write|generate|create|craft|construct|build|provide|give|show|use|run|apply|patch|bypass|remove|unlock|crack|extract|dump|recover)\b.{0,100}(?:crackme|reverse\s+engineering|ida(?:\s+pro)?|ghidra|x64dbg|ollydbg|frida|angr|jadx|apktool|unpack|decompile|disassemble|anti[-\s]?debug|anti[-\s]?tamper|license|activation|serial|keygen|signature\s+check).{0,100}(?:secret|key|token|password|license|activation|serial|check|bypass|patch|payload|script)|(?:破解|生成|构造|制作|提供|给我|使用|运行|应用|补丁|绕过|去除|解锁|提取|导出|恢复).{0,100}(?:逆向|IDA|Ghidra|反编译|反汇编|脱壳|反调试|反篡改|授权|激活|注册码|序列号|签名校验).{0,100}(?:密钥|令牌|密码|授权|激活|注册码|序列号|校验|绕过|补丁|脚本)`,
		Weight:   100,
		Category: "reverse_engineering",
		Strict:   true,
	},
	{
		Name:     "pentest_tooling",
		Regex:    `(?i)\b(?:ffuf|gobuster|dirsearch|feroxbuster|nikto|nuclei|impacket|bloodhound|netexec|crackmapexec|kerbrute|responder|rubeus|hydra|medusa|msfvenom|burp\s* suite|proxychains|chisel|ligolo|netcat|socat|enum4linux|amass|subfinder|httpx|assetfinder|waybackurls|metasploit|mimikatz|sqlmap|nmap|masscan|zmap|penetration\s+testing|pentest|red\s+team)\b|(?:目录扫描|子域名枚举|资产发现|端口扫描|密码喷洒|撞库|凭证转储|中继攻击|代理链|内网穿透|渗透|渗透测试工具)`,
		Weight:   25,
		Category: "pentest_tooling",
	},
	{
		Name:     "pentest_operational_request",
		Regex:    `(?i)\b(?:write|generate|create|craft|construct|build|provide|give|show|run|use|execute|scan|enumerate|exploit|brute[-\s]?force|spray|dump|relay|pivot|attack)\b.{0,100}(?:ffuf|gobuster|dirsearch|feroxbuster|nikto|nuclei|impacket|bloodhound|netexec|crackmapexec|kerbrute|responder|rubeus|hydra|medusa|msfvenom|burp\s* suite|proxychains|chisel|ligolo|netcat|socat|enum4linux|amass|subfinder|metasploit|mimikatz|sqlmap|nmap|masscan|zmap).{0,100}(?:public|internet|target|host|domain|network|account|credential|password|secret|server)|(?:写|生成|构造|制作|提供|给我|运行|使用|执行|扫描|枚举|利用|爆破|喷洒|转储|中继|横向|攻击).{0,100}(?:目录扫描|子域名枚举|资产发现|端口扫描|密码喷洒|撞库|凭证转储|中继攻击|内网穿透).{0,100}(?:公网|互联网|目标|主机|域名|网络|账号|凭证|密码|机密|服务器)`,
		Weight:   100,
		Category: "pentest_tooling",
		Strict:   true,
	},
	{
		Name:     "credential_attack_operational_request",
		Regex:    `(?i)(?:crack|dump|steal|extract|harvest|brute[-\s]?force|spray|pass[-\s]?the[-\s]?hash|pass[-\s]?the[-\s]?ticket|破解|转储|窃取|提取|收集|爆破|喷洒|哈希传递|票据传递).{0,100}(?:password|credential|hash|cookie|token|ticket|login|account|lsass|sam|ntds|mimikatz|hashcat|john|hydra|密码|凭证|哈希|cookie|令牌|票据|登录|账号|LSASS|SAM|NTDS).{0,100}(?:script|command|tool|payload|access|admin|root|脚本|命令|工具|载荷|访问|管理员|root)?`,
		Weight:   100,
		Category: "credential_attack",
		Strict:   true,
	},
}
