package promptfilter

// Supplemental local rules distilled from public OWASP LLM01 and CTF Handbook
// taxonomies. Topic rules create a semantic-review candidate; operational rules
// are intentionally narrower and are eligible for the existing hard block.
// Sources: https://genai.owasp.org/llmrisk/llm01-prompt-injection/ and
// https://ctf101.org/.
const supplementalSourceRevision = "local-content-audit@2026-08-08"

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
		Regex:    `(?i)\b(?:sqli|sql\s+injection|union\s+select|blind\s+sqli|time[-\s]?based\s+sqli|command\s+injection|os\s+command\s+injection|shell\s+injection|rce|remote\s+code\s+execution|ssrf|server[-\s]?side\s+request\s+forgery|ssti|server[-\s]?side\s+template\s+injection|xxe|xml\s+external\s+entity|xss|cross[-\s]?site\s+scripting|csrf|request\s+smuggling|prototype\s+pollution|insecure\s+deserialization|idor|bola|jwt\s+(?:attack|bypass|forgery)|oauth\s+redirect|open\s+redirect|cors\s+misconfig(?:uration)?|path\s+traversal|directory\s+traversal|lfi|rfi|file\s+inclusion|http\s+parameter\s+pollution)\b|(?:SQL\s*注入|命令注入|远程代码执行|服务端请求伪造|模板注入|XXE|反序列化|跨站脚本|跨站请求伪造|目录遍历|文件包含|原型链污染|越权|请求走私|参数污染|JWT攻击|OAuth重定向|开放重定向|CORS配置错误)`,
		Weight:   25,
		Category: "web_exploitation",
	},
	{
		Name:     "web_exploitation_unauthorized_harm_request",
		Regex:    `(?i)\b(?:write|generate|create|craft|construct|build|exploit|attack|bypass|dump|steal|exfiltrate)\b.{0,100}(?:sql\s+injection|sqli|union\s+select|command\s+injection|rce|ssrf|ssti|xss|csrf|xxe|deserialization|path\s+traversal|directory\s+traversal|lfi|idor|bola|jwt|prototype\s+pollution|request\s+smuggling).{0,120}(?:unauthori[sz]ed|without\s+(?:permission|authorization)|victim(?:'s)?|third[-\s]?party|someone\s+else(?:'s)?|compromised|production\s+(?:internal\s+admin|target|victim\s+service)).{0,120}(?:steal|dump|exfiltrat|credential|password|secret|cookie|token|root\s+shell|web\s+shell|take\s+control|access\s+(?:the\s+)?internal\s+admin)|\b(?:write|generate|create|craft|construct|build|exploit|attack|bypass|dump|steal|exfiltrate)\b.{0,100}(?:sql\s+injection|sqli|union\s+select|command\s+injection|rce|ssrf|ssti|xss|csrf|xxe|deserialization|path\s+traversal|directory\s+traversal|lfi|idor|bola|jwt|prototype\s+pollution|request\s+smuggling).{0,120}(?:steal|dump|exfiltrat|credential|password|secret|cookie|token|root\s+shell|web\s+shell|take\s+control|access\s+(?:the\s+)?internal\s+admin).{0,120}(?:unauthori[sz]ed|without\s+(?:permission|authorization)|victim(?:'s)?|third[-\s]?party|someone\s+else(?:'s)?|compromised|production\s+(?:internal\s+admin|target|victim\s+service))|(?:攻击|入侵|绕过(?:认证|授权|权限)|窃取|外传|导出|写|生成|构造|制作).{0,100}(?:SQL\s*注入|命令注入|RCE|SSRF|SSTI|XSS|CSRF|XXE|反序列化|目录遍历|文件包含|越权|JWT|原型链污染|请求走私).{0,120}(?:未授权|未经许可|受害者|第三方|他人|已失陷|生产内部后台|生产目标).{0,120}(?:窃取|外传|导出|凭证|密码|机密|cookie|token|root\s*shell|webshell|接管|访问内部后台)|(?:攻击|入侵|绕过(?:认证|授权|权限)|窃取|外传|导出|写|生成|构造|制作).{0,100}(?:SQL\s*注入|命令注入|RCE|SSRF|SSTI|XSS|CSRF|XXE|反序列化|目录遍历|文件包含|越权|JWT|原型链污染|请求走私).{0,120}(?:窃取|外传|导出|凭证|密码|机密|cookie|token|root\s*shell|webshell|接管|访问内部后台).{0,120}(?:未授权|未经许可|受害者|第三方|他人|已失陷|生产内部后台|生产目标)`,
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
		Name:     "binary_exploitation_unauthorized_harm_request",
		Regex:    `(?i)\b(?:write|generate|create|craft|construct|build|exploit|hijack)\b.{0,100}(?:buffer\s+overflow|stack\s+overflow|heap\s+overflow|use[-\s]?after[-\s]?free|uaf|double\s+free|format\s+string|ret2libc|ret2csu|ret2dlresolve|rop|return[-\s]?oriented|shellcode|tcache|got\s+overwrite|pwntools).{0,120}(?:unauthori[sz]ed|without\s+(?:permission|authorization)|victim(?:'s)?|third[-\s]?party|someone\s+else(?:'s)?|production\s+(?:remote\s+)?(?:target|host|service)).{0,120}(?:steal|exfiltrat|root\s+shell|persistent\s+shell|take\s+control|hijack\s+execution)|\b(?:write|generate|create|craft|construct|build|exploit|hijack)\b.{0,100}(?:buffer\s+overflow|stack\s+overflow|heap\s+overflow|use[-\s]?after[-\s]?free|uaf|double\s+free|format\s+string|ret2libc|ret2csu|ret2dlresolve|rop|return[-\s]?oriented|shellcode|tcache|got\s+overwrite|pwntools).{0,120}(?:steal|exfiltrat|root\s+shell|persistent\s+shell|take\s+control|hijack\s+execution).{0,120}(?:unauthori[sz]ed|without\s+(?:permission|authorization)|victim(?:'s)?|third[-\s]?party|someone\s+else(?:'s)?|production\s+(?:remote\s+)?(?:target|host|service))|(?:利用|劫持|构造|制作).{0,100}(?:栈溢出|缓冲区溢出|堆溢出|格式化字符串|返回导向|释放后使用|堆利用|PWN|shellcode).{0,120}(?:未授权|未经许可|受害者|第三方|他人|生产(?:远程)?(?:目标|主机|服务)).{0,120}(?:窃取|外传|root\s*shell|持久化shell|控制服务器|劫持执行)|(?:利用|劫持|构造|制作).{0,100}(?:栈溢出|缓冲区溢出|堆溢出|格式化字符串|返回导向|释放后使用|堆利用|PWN|shellcode).{0,120}(?:窃取|外传|root\s*shell|持久化shell|控制服务器|劫持执行).{0,120}(?:未授权|未经许可|受害者|第三方|他人|生产(?:远程)?(?:目标|主机|服务))`,
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
		Name:     "crypto_unauthorized_key_theft_request",
		Regex:    `(?i)\b(?:crack|break|decrypt|recover|derive|brute[-\s]?force|forge|steal)\b.{0,100}(?:private\s+key|password|credential|secret|authentication\s+token|wallet\s+key|signing\s+key).{0,100}(?:unauthori[sz]ed|without\s+(?:permission|authorization)|victim(?:'s)?|third[-\s]?party|someone\s+else(?:'s)?|stolen|compromised)|\b(?:crack|break|decrypt|recover|derive|brute[-\s]?force|forge|steal)\b.{0,100}(?:unauthori[sz]ed|without\s+(?:permission|authorization)|victim(?:'s)?|third[-\s]?party|someone\s+else(?:'s)?|stolen|compromised).{0,100}(?:private\s+key|password|credential|secret|authentication\s+token|wallet\s+key|signing\s+key)|\b(?:unauthori[sz]ed|without\s+(?:permission|authorization)|victim(?:'s)?|third[-\s]?party|someone\s+else(?:'s)?|stolen|compromised)\b.{0,100}(?:crack|break|decrypt|recover|derive|brute[-\s]?force|forge|steal).{0,100}(?:private\s+key|password|credential|secret|authentication\s+token|wallet\s+key|signing\s+key)|(?:破解|解密|恢复|推导|爆破|伪造|窃取).{0,100}(?:私钥|密码|凭证|机密|认证令牌|钱包密钥|签名密钥).{0,100}(?:未授权|未经许可|受害者|第三方|他人|被盗|已失陷)|(?:破解|解密|恢复|推导|爆破|伪造|窃取).{0,100}(?:未授权|未经许可|受害者|第三方|他人|被盗|已失陷).{0,100}(?:私钥|密码|凭证|机密|认证令牌|钱包密钥|签名密钥)|(?:未授权|未经许可|受害者|第三方|他人|被盗|已失陷).{0,100}(?:破解|解密|恢复|推导|爆破|伪造|窃取).{0,100}(?:私钥|密码|凭证|机密|认证令牌|钱包密钥|签名密钥)`,
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
		Name:     "reverse_engineering_sensitive_candidate",
		Regex:    `(?i)\b(?:ida(?:\s+pro)?|ghidra|x64dbg|ollydbg|frida|jadx|apktool|decompil(?:e|er)|disassembl(?:e|y)|reverse\s+engineer(?:ing)?)\b.{0,120}\b(?:extract|dump|recover|decrypt|steal)\b.{0,80}\b(?:api\s*keys?|tokens?|secrets?|private\s*keys?|passwords?|certificates?|license\s*keys?)\b|\b(?:ida(?:\s+pro)?|ghidra|x64dbg|ollydbg|frida|jadx|apktool|decompil(?:e|er)|disassembl(?:e|y)|reverse\s+engineer(?:ing)?)\b.{0,120}\b(?:bypass|crack|patch|remove|unlock)\b.{0,80}\b(?:license|activation|trial|paywall|subscription|in[-\s]?app\s+purchase|iap|entitlement|signature\s+check)\b|(?:IDA|Ghidra|x64dbg|Frida|JADX|APKTool|反编译|反汇编).{0,100}(?:提取|导出|解密|恢复|窃取).{0,50}(?:API密钥|密钥|令牌|密码|私钥|证书|授权码)|(?:IDA|Ghidra|x64dbg|Frida|JADX|APKTool|反编译|反汇编|逆向).{0,100}(?:绕过|破解|补丁|去除|解锁).{0,50}(?:授权|激活|试用|付费墙|会员|订阅|内购|权益|签名校验)`,
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
		Name:     "pentest_unauthorized_harm_request",
		Regex:    `(?i)\b(?:run|use|execute)\b.{0,80}(?:ffuf|gobuster|dirsearch|feroxbuster|nikto|nuclei|impacket|bloodhound|netexec|crackmapexec|kerbrute|responder|rubeus|hydra|medusa|msfvenom|metasploit|mimikatz|sqlmap).{0,100}\b(?:exploit|attack|brute[-\s]?force|password\s+spray|credential\s+dump|relay|steal|exfiltrate)\b.{0,80}\b(?:target|victim|host|domain|network|account|credential|password|server)\b|\b(?:scan|enumerate)\b.{0,80}(?:nmap|masscan|zmap|amass|subfinder|ffuf|gobuster|dirsearch|feroxbuster|nuclei)?.{0,80}\b(?:internet[-\s]?wide|public\s+(?:ip|host|server|range)|mass|bulk|unauthori[sz]ed|without\s+(?:permission|authorization)|third[-\s]?party)\b.{0,80}\b(?:target|host|domain|network|server|range|address)\b|(?:运行|使用|执行).{0,60}(?:Hydra|Medusa|Impacket|Mimikatz|Metasploit|SQLMap|Responder|Rubeus).{0,80}(?:爆破|密码喷洒|凭证转储|中继攻击|窃取|攻击|利用).{0,60}(?:目标|受害者|主机|域名|网络|账号|凭证|密码|服务器)|(?:扫描|枚举).{0,80}(?:公网|互联网|批量|大规模|未授权|未经许可|第三方).{0,60}(?:目标|IP|地址|主机|域名|网段|网络|服务器)`,
		Weight:   100,
		Category: "pentest_tooling",
		Strict:   true,
	},
	{
		Name:     "credential_attack_operational_request",
		Regex:    `(?i)\b(?:crack|dump|steal|extract|harvest|brute[-\s]?force|spray|pass[-\s]?the[-\s]?hash|pass[-\s]?the[-\s]?ticket)\b.{0,100}\b(?:password|credential|hash|cookie|token|ticket|login|account|lsass|sam|ntds|mimikatz|hashcat|john|hydra)\b.{0,120}\b(?:unauthori[sz]ed|without\s+(?:permission|authorization)|victim|third[-\s]?party|someone\s+else(?:'s)?|stolen|compromised|production\s+(?:account|system|server)|admin\s+access|root\s+access)\b|\b(?:unauthori[sz]ed|without\s+(?:permission|authorization)|victim|third[-\s]?party|someone\s+else(?:'s)?|stolen|compromised|production)\b.{0,100}\b(?:crack|dump|steal|extract|harvest|brute[-\s]?force|spray|pass[-\s]?the[-\s]?hash|pass[-\s]?the[-\s]?ticket)\b.{0,100}\b(?:password|credential|hash|cookie|token|ticket|login|account|lsass|sam|ntds)\b|(?:破解|转储|窃取|提取|收集|爆破|喷洒|哈希传递|票据传递).{0,100}(?:密码|凭证|哈希|cookie|令牌|票据|登录|账号|LSASS|SAM|NTDS).{0,100}(?:未经授权|未授权|无权限|受害者|第三方|他人|被盗|已失陷|生产账号|生产系统|管理员权限|root权限)|(?:未经授权|未授权|无权限|受害者|第三方|他人|被盗|已失陷|生产环境).{0,100}(?:破解|转储|窃取|提取|收集|爆破|喷洒|哈希传递|票据传递).{0,100}(?:密码|凭证|哈希|cookie|令牌|票据|登录|账号|LSASS|SAM|NTDS)`,
		Weight:   100,
		Category: "credential_attack",
		Strict:   true,
	},
	{
		Name:     "ransomware_creation_request",
		Regex:    `(?i)\b(?:write|generate|create|build|develop|implement|provide)\s+(?:me\s+)?(?:a\s+)?(?:(?:fully\s+)?(?:functional|working)|deployable|complete|production[-\s]?ready)\s+ransomware\s+(?:program|payload|code|script|tool|sample)\b.{0,180}\b(?:encrypt(?:s|ing)?\s+(?:(?:victim|target|user)(?:'s)?\s+|all\s+)?(?:files?|documents?|data)|drop(?:s|ping)?\s+(?:a\s+)?ransom\s+note|demand(?:s|ing)?\s+(?:a\s+)?(?:ransom|payment)|delete(?:s|ing)?\s+(?:backups?|shadow\s+copies)|spread(?:s|ing)?|propagat(?:e|es|ing)|infect(?:s|ing)?)\b|(?:写|编写|生成|创建|制作|开发|实现|提供)(?:给我)?(?:一个|一份|一套)?(?:完整|可用|可运行|可部署|功能齐全){1,2}(?:的)?(?:勒索软件|勒索病毒)(?:程序|载荷|代码|脚本|工具|样本).{0,140}(?:加密(?:受害者|目标|用户|所有)?(?:的)?(?:文件|文档|数据)|投放勒索信|索要(?:赎金|付款)|删除(?:备份|卷影副本)|传播|感染)`,
		Weight:   100,
		Category: "malware",
		Strict:   true,
	},
	{
		Name:     "covert_surveillance_privacy_abuse_request",
		Regex:    `(?i)\b(?:help\s+me|show\s+me\s+how\s+to|tell\s+me\s+how\s+to|how\s+(?:can|do)\s+i|i\s+want\s+to|provide\s+(?:detailed\s+)?(?:instructions|steps|a\s+plan)\s+(?:for|to)|give\s+me\s+(?:a\s+)?(?:plan|steps)\s+to)\s+(?:secretly|covertly|silently|without\s+(?:their\s+)?(?:knowledge|consent))\s*(?:track|monitor|surveil|spy\s+on|intercept|tap)\b.{0,180}\b(?:private|personal)?\s*(?:communications?|messages?|calls?|texts?|emails?|location|home\s+address)\b|\b(?:help\s+me|show\s+me\s+how\s+to|tell\s+me\s+how\s+to|how\s+(?:can|do)\s+i|i\s+want\s+to|provide\s+(?:detailed\s+)?(?:instructions|steps|a\s+plan)\s+(?:for|to)|give\s+me\s+(?:a\s+)?(?:plan|steps)\s+to)\s+(?:track|monitor|surveil|spy\s+on|intercept|tap)\b.{0,180}\b(?:private|personal)?\s*(?:communications?|messages?|calls?|texts?|emails?|location|home\s+address)\b.{0,180}\b(?:avoid(?:ing)?\s+detection|without\s+being\s+detected|undetected|leave\s+no\s+trace|without\s+(?:their\s+)?(?:knowledge|consent))\b|(?:帮我|教我|告诉我如何|给我(?:一个|一份)?(?:方案|步骤)|如何|怎么|我想(?:要)?)(?:秘密|暗中|隐蔽|偷偷|在(?:本人|对方)不知情时|未经(?:本人|对方)?同意)(?:跟踪|追踪|监视|监控|监听|窃听|拦截|截取).{0,140}(?:私人通信|个人通信|消息|通话|短信|邮件|位置|家庭住址|住址|地址)|(?:帮我|教我|告诉我如何|给我(?:一个|一份)?(?:方案|步骤)|如何|怎么|我想(?:要)?)(?:跟踪|追踪|监视|监控|监听|窃听|拦截|截取).{0,140}(?:私人通信|个人通信|消息|通话|短信|邮件|位置|家庭住址|住址|地址).{0,140}(?:避开|规避)(?:检测|侦测)|(?:帮我|教我|告诉我如何|给我(?:一个|一份)?(?:方案|步骤)|如何|怎么|我想(?:要)?)(?:跟踪|追踪|监视|监控|监听|窃听|拦截|截取).{0,140}(?:私人通信|个人通信|消息|通话|短信|邮件|位置|家庭住址|住址|地址).{0,140}(?:不被发现|不留痕迹|未经(?:本人|对方)?同意|在(?:本人|对方)不知情)`,
		Weight:   100,
		Category: "privacy",
		Strict:   true,
	},
	{
		Name:     "protocol_entitlement_bypass_request",
		Regex:    `(?i)\b(?:help\s+me|show\s+me\s+how\s+to|tell\s+me\s+how\s+to|how\s+(?:can|do)\s+i|i\s+want\s+to|give\s+me\s+(?:a\s+)?(?:plan|steps)\s+to|provide\s+(?:instructions|steps)\s+to)\s+(?:intercept|hijack|tamper\s+with|modify|rewrite|forge|spoof)\b.{0,140}\b(?:gateway|server|client|engine|protocol|traffic|communication|request|response|handshake|state)\b.{0,180}\b(?:bypass|circumvent|evade|defeat|remove|raise)\b.{0,100}\b(?:licen[cs](?:e|ing)|activation|seat|user|device|concurren(?:t|cy)|usage|subscription|entitlement)\b.{0,40}\b(?:limit|cap|restriction|check|quota)\b|\b(?:write|build|create|design|implement)\s+(?:a\s+)?(?:tool|script|proxy|method|plan|system)\s+to\s+(?:intercept|hijack|tamper\s+with|modify|rewrite|forge|spoof)\b.{0,140}\b(?:gateway|server|client|engine|protocol|traffic|communication|request|response|handshake|state)\b.{0,180}\b(?:bypass|circumvent|evade|defeat|remove|raise)\b.{0,100}\b(?:licen[cs](?:e|ing)|activation|seat|user|device|concurren(?:t|cy)|usage|subscription|entitlement)\b.{0,40}\b(?:limit|cap|restriction|check|quota)\b|(?:帮我|教我|告诉我如何|如何|怎么|我想(?:要)?)(?:通过)?(?:拦截|劫持|篡改|修改|伪造|重写).{0,100}(?:网关|服务器|客户端|引擎|协议|通信|请求|响应|握手|状态).{0,140}(?:绕过|突破|规避|解除|提高).{0,80}(?:授权|许可|激活|席位|用户|人数|设备|并发|用量|订阅|权益).{0,30}(?:限制|上限|校验|配额)|(?:设计|实现|编写)(?:一个|一套)?(?:代理|脚本|工具|方案|系统)(?:来|去|用于)?(?:拦截|劫持|篡改|修改|伪造|重写).{0,100}(?:网关|服务器|客户端|引擎|协议|通信|请求|响应|握手|状态).{0,140}(?:绕过|突破|规避|解除|提高).{0,80}(?:授权|许可|激活|席位|用户|人数|设备|并发|用量|订阅|权益).{0,30}(?:限制|上限|校验|配额)`,
		Weight:   100,
		Category: "license_cracking",
		Strict:   true,
	},
}
