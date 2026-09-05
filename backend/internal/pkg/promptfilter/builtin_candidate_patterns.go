package promptfilter

// Candidate rules are deliberately non-strict and non-operational. They widen
// local recall for candidate_only, but only a downstream reviewer can decide
// whether the request is allowed or blocked.
const candidateSourceRevision = "local-candidate-recall@2026-09-06-cyber-intent"

// Expiry and quota maintenance is ordinary application work. Broad entitlement
// terms require circumvention verbs; edits to licensing checks remain candidates.
const entitlementCircumventionEN = `\b(?:bypass|circumvent|disable|remove|unlock|patch|hook|tamper\s+with|forge|spoof|fake|force)\b`
const entitlementTargetEN = `\b(?:licen[cs](?:e|ing)|activation|trial|paywall|subscription|entitlement|expiry|expiration|validity|valid\s+days?|(?:seat|user|device|concurren(?:t|cy)|usage)\s+(?:limit|cap|restriction|check|quota))\b`
const entitlementEditEN = `\b(?:extend|reset|return|set|change|modify)\b`
const entitlementLicenseEN = `\b(?:licen[cs](?:e|ing)|activation|trial|paywall|entitlement)\b`
const entitlementCircumventionZH = `(?:绕过|突破|规避|解除|移除|去除|伪造|篡改|(?:固定|直接)返回)`
const entitlementTargetZH = `(?:授权|许可|激活|试用|订阅|会员|付费墙|权益|有效期|有效天数|到期时间|过期时间|(?:席位|人数|用户数|设备数|并发|用量).{0,12}(?:限制|上限|校验|检查|配额))`
const entitlementEditZH = `(?:提高|修改|改成|改为|延长|重置|返回)`
const entitlementLicenseZH = `(?:(?:授权|许可).{0,8}(?:校验|验证|检查|限制|期限)|激活|试用|付费墙)`

var candidatePatternConfigs = append([]PatternConfig{
	{
		Name:   "candidate_virology_intent",
		Regex:  `(?i)\b(?:virolog(?:y|ic|ical|ists?)|viral|virus(?:es)?|virions?|pathogens?|bacteriophages?|coronavirus(?:es)?|influenza|sars[-\s]?cov(?:[-\s]?2)?|hiv|hbv|hcv|hpv|ebola|bioweapons?|biological\s+(?:agents?|weapons?))\b|病毒|病原体|噬菌体|生物武器|流感病毒|冠状病毒`,
		Weight: 75, Category: "biosecurity",
	},
	{
		Name:   "candidate_reverse_engineering_intent",
		Regex:  `(?i)\b(?:reverse[-\s]*engineer(?:ing|ed|s)?|decompil(?:e[ds]?|ers?|ing|ation)|disassembl(?:e[ds]?|ers?|ing|y)|ghidra|ida\s+pro|x64dbg|ollydbg|frida|jadx|apktool|dnspy|ilspy|radare2|retdec|keygens?)\b|逆向|反编译|反汇编|反混淆|去混淆|脱壳|注册机|破解补丁|软件破解|破解软件`,
		Weight: 75, Category: "reverse_engineering",
	},
	{
		Name:   "candidate_cyber_attack_intent",
		Regex:  `(?i)\b(?:cyber[-\s]?attacks?|ddos|botnets?|credential\s+(?:stuffing|harvesting)|password\s+spraying|session\s+hijacking|data\s+exfiltration|ransomware|infostealers?|ransomware[-\s]as[-\s]a[-\s]service|unauthori[sz]ed\s+intrusion)\b|网络攻击|拒绝服务攻击|僵尸网络|撞库|凭证填充|密码喷洒|会话劫持|窃取凭证|窃取令牌|数据外传|勒索软件|盗号木马`,
		Weight: 70, Category: "cyber",
	},
	{
		Name: "candidate_unauthorized_access",
		Regex: `(?i)\b(?:break\s+into|hack\s+into|gain\s+unauthori[sz]ed\s+access\s+to|access\s+without\s+(?:permission|authorization)|bypass\s+(?:the\s+)?(?:login|authentication|access\s+control))\b.{0,100}\b(?:account|email|server|system|network|database|device|admin|portal)\b|(?:入侵|黑进|未授权访问|未经授权访问|绕过(?:登录|认证|鉴权|访问控制)).{0,80}(?:账号|邮箱|服务器|系统|网络|数据库|设备|后台)|` +
			`\b(?:attack|compromise|take\s+over)\b.{0,80}\b(?:third[-\s]?party|victim|someone\s+else(?:'s)?|unauthori[sz]ed)\b.{0,60}\b(?:servers?|hosts?|websites?|accounts?|networks?)\b|` +
			`(?:攻击|打穿|拿下|接管).{0,60}(?:他人|第三方|受害者|未授权).{0,60}(?:网站|主机|服务器|账号|内网|域控)`,
		Weight:   70,
		Category: "cyber",
	},
	{
		Name:     "candidate_account_abuse",
		Regex:    `(?i)\b(?:create|register|farm|buy|sell|obtain|take\s+over)\b.{0,80}\b(?:bulk|fake|stolen|compromised|aged|verified)\b.{0,50}\baccounts?\b|\b(?:evade|bypass)\b.{0,40}\b(?:account\s+ban|suspension|platform\s+limit)\b|(?:批量注册|养号|购买|出售|接管|盗取).{0,60}(?:虚假|被盗|黑产|实名|老号)?(?:账号|账户)|绕过.{0,30}(?:封号|账号封禁|平台限制)`,
		Weight:   65,
		Category: "account_abuse",
	},
	{
		Name:     "candidate_privacy_doxxing",
		Regex:    `(?i)\b(?:doxx?|deanonymi[sz]e|unmask|track\s+down|find|locate|reveal|publish)\b.{0,100}\b(?:someone(?:'s)?|a\s+private\s+person(?:'s)?|this\s+person(?:'s)?|their)\b.{0,60}\b(?:home\s+address|phone\s+number|current\s+location|real\s+identity|private\s+records?)\b|\b(?:track|monitor|surveil|spy\s+on|intercept|tap)\b.{0,120}\b(?:someone|a\s+person|another\s+person|this\s+person|their)\b.{0,100}\b(?:communications?|messages?|calls?|texts?|emails?|location|home\s+address)\b|(?:人肉|开盒|去匿名|查找|定位|曝光|公布).{0,80}(?:某人|个人|这个人|他人).{0,50}(?:家庭住址|手机号|当前位置|真实身份|隐私记录)|(?:跟踪|追踪|监视|监控|监听|窃听|拦截|截取).{0,100}(?:某人|个人|这个人|他人|对方|他|她).{0,80}(?:私人通信|个人通信|消息|通话|短信|邮件|位置|家庭住址|住址|地址)`,
		Weight:   70,
		Category: "privacy",
	},
	{
		Name:     "candidate_hate_abuse",
		Regex:    `(?i)\b(?:write|create|generate|spread|publish|target|incite)\b.{0,100}\b(?:hate\s+propaganda|racial\s+slurs?|ethnic\s+slurs?|dehumani[sz]ing\s+(?:content|messages?)|violence\s+against\s+(?:a\s+)?protected\s+group)\b|(?:写|生成|制作|传播|发布|煽动).{0,80}(?:仇恨宣传|种族辱骂|民族辱骂|贬低特定群体|针对受保护群体的暴力)`,
		Weight:   65,
		Category: "other",
	},
	{
		Name:     "candidate_illegal_goods_transaction",
		Regex:    `(?i)\b(?:buy|sell|source|ship|smuggle|find\s+(?:a\s+)?(?:seller|vendor)|connect\s+me\s+with)\b.{0,100}\b(?:illegal\s+drugs?|cocaine|meth(?:amphetamine)?|fentanyl|stolen\s+(?:credit\s+)?cards?|fake\s+(?:ids?|passports?|prescriptions?)|counterfeit\s+currency)\b|(?:购买|出售|采购|走私|邮寄|找(?:卖家|渠道)|联系卖家).{0,80}(?:毒品|可卡因|冰毒|芬太尼|盗刷卡|被盗银行卡|假证|假护照|假处方|假币)`,
		Weight:   70,
		Category: "fraud",
	},
	{
		Name:     "candidate_high_impact_discrimination",
		Regex:    `(?i)\b(?:rank|score|screen|reject|deny|exclude|approve|select)\b.{0,120}\b(?:job\s+applicants?|employees?|borrowers?|loan\s+applicants?|tenants?|students?|patients?|insurance\s+applicants?)\b.{0,120}\b(?:race|ethnicity|religion|sex|gender|sexual\s+orientation|disability|medical\s+history|pregnancy|nationality)\b|(?:根据|基于).{0,50}(?:种族|民族|宗教|性别|性取向|残疾|病史|怀孕|国籍).{0,100}(?:筛选|排名|拒绝|淘汰|批准|录用).{0,60}(?:求职者|员工|借款人|贷款申请人|租客|学生|患者|投保人)`,
		Weight:   75,
		Category: "high_impact_decision",
	},
	{
		Name:     "candidate_sexual_exploitation",
		Regex:    `(?i)\b(?:write|create|generate|make|produce)\b.{0,100}\b(?:explicit\s+porn(?:ography)?|non[-\s]?consensual\s+(?:sexual|intimate)\s+content|deepfake\s+nudes?|sexual\s+content\s+involving\s+minors?|child\s+sexual\s+abuse\s+material|csam)\b|(?:写|生成|制作|合成).{0,80}(?:露骨色情|成人视频|非自愿色情|私密影像|深度伪造裸照|未成年人色情|儿童性虐待材料)`,
		Weight:   80,
		Category: "other",
	},
	{
		Name:     "candidate_self_harm_actionable",
		Regex:    `(?i)\b(?:give|provide|list|calculate|recommend|compare|optimi[sz]e|help\s+me\s+hide)\b.{0,100}\b(?:suicide\s+methods?|ways?\s+to\s+die|lethal\s+dose|fatal\s+dose|self[-\s]?harm\s+methods?|signs?\s+of\s+self[-\s]?harm)\b|(?:给我|提供|列出|计算|推荐|比较|优化|帮我隐藏).{0,80}(?:自杀方法|死亡方式|致死剂量|自残方法|自残痕迹)`,
		Weight:   80,
		Category: "self_harm",
	},
	{
		Name:     "candidate_political_manipulation",
		Regex:    `(?i)\b(?:microtarget|profile|persuade|manipulate|mislead|suppress|discourage|astroturf)\b.{0,100}\b(?:voters?|electorate|election|ballot|political\s+supporters?|swing\s+voters?)\b|(?:微定向|画像|说服|操纵|误导|压制|劝阻|制造虚假民意).{0,80}(?:选民|选举|投票|政治支持者|摇摆选民)`,
		Weight:   70,
		Category: "political",
	},
	{
		Name:     "candidate_weapon_construction",
		Regex:    `(?i)\b(?:build|make|assemble|construct|manufacture|modify)\b.{0,100}\b(?:bomb|improvised\s+explosive|ied|detonator|explosive\s+device|ghost\s+gun|untraceable\s+firearm|automatic\s+weapon|silencer)\b|(?:制造|制作|组装|改装).{0,80}(?:炸弹|简易爆炸装置|起爆器|爆炸物|幽灵枪|无序列号枪支|自动武器|消音器)`,
		Weight:   80,
		Category: "weapons",
	},
	{
		Name:     "candidate_subscription_crypto_extraction",
		Regex:    `(?i)(?:\b(?:blutter|flutter|dart\s+snapshot)\b|订阅加密|subscription\s+encryption).{0,220}(?:\b(?:extract|dump|recover|derive|decrypt|analy[sz]e)\b|提取|导出|恢复|推导|解密|分析).{0,140}(?:\b(?:key\s*(?:and|&|/|和)\s*iv|key/iv|initiali[sz]ation\s+vector|crypto(?:graphic)?\s+key)\b|密钥.{0,20}(?:iv|初始化向量)|初始化向量)|(?:\b(?:extract|dump|recover|derive|decrypt|analy[sz]e)\b|提取|导出|恢复|推导|解密|分析).{0,140}(?:\b(?:key\s*(?:and|&|/|和)\s*iv|key/iv|initiali[sz]ation\s+vector|crypto(?:graphic)?\s+key)\b|密钥.{0,20}(?:iv|初始化向量)|初始化向量).{0,220}(?:\b(?:blutter|flutter|dart\s+snapshot)\b|订阅加密|subscription\s+encryption)`,
		Weight:   65,
		Category: "cyber",
	},
	{
		Name:     "candidate_key_iv_extraction",
		Regex:    `(?i)(?:\b(?:extract|dump|recover|derive|decrypt|find|obtain)\b|提取|导出|恢复|推导|解密|查找|获取).{0,100}(?:\b(?:key\s*(?:and|&|/|和)\s*iv|key/iv|initiali[sz]ation\s+vector|crypto(?:graphic)?\s+key)\b|密钥|密码|令牌|token|私钥|初始化向量)|(?:\b(?:key\s*(?:and|&|/|和)\s*iv|key/iv|initiali[sz]ation\s+vector|crypto(?:graphic)?\s+key)\b|密钥|密码|令牌|token|私钥|初始化向量).{0,100}(?:\b(?:extract|dump|recover|derive|decrypt|find|obtain)\b|提取|导出|恢复|推导|解密|查找|获取)`,
		Weight:   55,
		Category: "cyber",
	},
	{
		Name: "candidate_software_entitlement_bypass",
		Regex: `(?i)\b(?:crack|brute[-\s]?force)\b.{0,60}\b(?:commercial\s+)?(?:software|program|application|app|client|binary|executable|exe|dll)\b|\b(?:commercial\s+)?(?:software|program|application|app|client|binary|executable|exe|dll)\b.{0,60}\b(?:crack|brute[-\s]?force)\b|\b(?:patch|hook|tamper\s+with|modify)\b.{0,60}\b(?:binary|executable|exe|dll)\b|\b(?:binary|executable|exe|dll)\b.{0,60}\b(?:patch|hook|tamper\s+with|modify)\b|` +
			entitlementCircumventionEN + `.{0,240}` + entitlementTargetEN + `|` + entitlementTargetEN + `.{0,240}` + entitlementCircumventionEN + `|` +
			entitlementEditEN + `.{0,100}` + entitlementLicenseEN + `|` + entitlementLicenseEN + `.{0,100}` + entitlementEditEN + `|` +
			`(?:破解|爆破).{0,60}(?:商业)?(?:软件|程序|应用|客户端|二进制(?:文件)?|EXE|DLL)|(?:商业)?(?:软件|程序|应用|客户端|二进制(?:文件)?|EXE|DLL).{0,60}(?:破解|爆破)|(?:patch|hook|打补丁|修改|篡改).{0,60}(?:二进制(?:文件)?|EXE|DLL)|(?:二进制(?:文件)?|EXE|DLL).{0,60}(?:patch|hook|打补丁|修改|篡改)|` +
			entitlementCircumventionZH + `.{0,240}` + entitlementTargetZH + `|` + entitlementTargetZH + `.{0,240}` + entitlementCircumventionZH + `|` +
			entitlementEditZH + `.{0,80}` + entitlementLicenseZH + `|` + entitlementLicenseZH + `.{0,80}` + entitlementEditZH,
		Weight:   70,
		Category: "license_cracking",
	},
	{
		Name:     "candidate_ransomware_creation",
		Regex:    `(?i)\b(?:write|generate|create|build|develop|implement|provide)\s+(?:me\s+)?(?:a\s+)?(?:(?:fully\s+)?(?:functional|working)|deployable|complete|production[-\s]?ready)?\s*ransomware(?:\s+(?:program|payload|code|script|tool|sample))?\b.{0,180}\b(?:encrypt(?:s|ing)?\s+(?:(?:victim|target|user)(?:'s)?\s+|all\s+)?(?:files?|documents?|data)|drop(?:s|ping)?\s+(?:a\s+)?ransom\s+note|demand(?:s|ing)?\s+(?:a\s+)?(?:ransom|payment)|delete(?:s|ing)?\s+(?:backups?|shadow\s+copies)|spread(?:s|ing)?|propagat(?:e|es|ing)|infect(?:s|ing)?)\b|(?:写|编写|生成|创建|制作|开发|实现|提供)(?:给我)?(?:一个|一份|一套)?(?:完整|可用|可运行|可部署|功能齐全)?(?:的)?(?:勒索软件|勒索病毒)(?:程序|载荷|代码|脚本|工具|样本)?.{0,140}(?:加密(?:受害者|目标|用户|所有)?(?:的)?(?:文件|文档|数据)|投放勒索信|索要(?:赎金|付款)|删除(?:备份|卷影副本)|传播|感染)`,
		Weight:   80,
		Category: "malware",
	},
	{
		Name:     "candidate_protocol_entitlement_bypass",
		Regex:    `(?i)\b(?:intercept|hijack|tamper\s+with|modify|rewrite|forge|spoof)\b.{0,140}\b(?:gateway|server|client|engine|protocol|traffic|communication|request|response|handshake|state)\b.{0,180}\b(?:bypass|circumvent|evade|defeat|remove|raise)\b.{0,100}\b(?:licen[cs](?:e|ing)|activation|seat|user|device|concurren(?:t|cy)|usage|subscription|entitlement)\b.{0,40}\b(?:limit|cap|restriction|check|quota)\b|(?:拦截|劫持|篡改|修改|伪造|重写).{0,100}(?:网关|服务器|客户端|引擎|协议|通信|请求|响应|握手|状态).{0,140}(?:绕过|突破|规避|解除|提高).{0,80}(?:授权|许可|激活|席位|用户|人数|设备|并发|用量|订阅|权益).{0,30}(?:限制|上限|校验|配额)`,
		Weight:   75,
		Category: "cyber",
	},
	{
		Name:     "candidate_financial_fraud",
		Regex:    `(?i)\b(?:fraud(?:ulent(?:ly)?)?|defraud(?:ed|ing|s)?|scam(?:s|med|ming|mers?)?|investment\s+scams?|romance\s+scams?|advance[-\s]fee\s+fraud|money\s+laundering|carding|stolen\s+card\s+cashout)\b|诈骗|洗钱|杀猪盘|电诈话术|(?:编写|生成|设计|制作|提供).{0,60}骗取转账|盗刷套现`,
		Weight:   70,
		Category: "fraud",
	},
	{
		Name:     "candidate_document_forgery",
		Regex:    `(?i)\b(?:forg(?:e[ds]?|ing|ery)|falsif(?:y|ies|ied|ying|ication)|counterfeit(?:ed|ing|s)?|fabricat(?:e[ds]?|ing|ion)|fake[ds]?)\b.{0,80}\b(?:invoices?|receipts?|bank\s+statements?|passports?|identity\s+documents?|prescriptions?|diplomas?|certificates?|signatures?|transaction\s+records?)\b|(?:伪造|造假|篡改|虚假).{0,60}(?:发票|收据|银行流水|身份证|护照|处方|学历|毕业证|证书|签名|印章|交易记录)|假(?:发票|银行流水|证件|合同)`,
		Weight:   70,
		Category: "fraud",
	},
	{
		Name:     "candidate_identity_impersonation",
		Regex:    `(?i)\b(?:impersonate|pose\s+as|pretend\s+to\s+be)\b.{0,70}\b(?:bank|police|customer\s+support|technical\s+support|government\s+official|employer|coworker)\b|\b(?:clone\s+(?:someone(?:'s)?|their)\s+voice|voice\s+cloning|deepfake)\b.{0,100}\b(?:deceive|defraud|steal|transfer\s+money|bypass\s+verification)\b|冒充.{0,40}(?:银行|客服|公安|警察|公检法|政府人员|同事)|(?:克隆声音|声音克隆|深度伪造|伪造身份).{0,80}(?:骗取|诈骗|转账|绕过验证|冒领)`,
		Weight:   70,
		Category: "fraud",
	},
	{
		Name:     "candidate_child_exploitation",
		Regex:    `(?i)\b(?:csam|(?:child(?:ren)?|minors?)\s+(?:pornography|sexual\s+(?:abuse|exploitation))|sexual\s+grooming\s+(?:of\s+)?(?:children|minors)|groom(?:ing)?\s+(?:a\s+)?(?:child|minor)\s+for\s+sex)\b|(?:儿童|幼童|未成年人?)性(?:虐待|剥削)|未成年人?色情|幼童色情|性诱骗.{0,20}(?:儿童|未成年人?)|(?:诱骗|胁迫).{0,40}(?:儿童|未成年人?).{0,40}(?:裸照|性行为|色情)`,
		Weight:   80,
		Category: "minor_safety",
	},
	{
		Name:     "candidate_coercion_violence",
		Regex:    `(?i)\b(?:plan|organize|carry\s+out|help\s+with)\b.{0,80}\b(?:terrorist\s+attacks?|mass\s+violence|assassination|kidnapping)\b|\b(?:blackmail|extort|intimidate)\b.{0,60}\b(?:someone|a\s+person|the\s+victim|my\s+(?:partner|coworker|neighbor))\b|(?:策划|组织|实施|协助).{0,60}(?:恐怖袭击|暴力袭击|暗杀|绑架)|(?:勒索|敲诈|恐吓).{0,40}(?:某人|他人|对方|受害者|同事|邻居)`,
		Weight:   80,
		Category: "violence",
	},
	{
		Name:     "candidate_chemical_radiological_weapons",
		Regex:    `(?i)\b(?:chemical\s+weapons?|nerve\s+agents?|radiological\s+weapons?|dirty\s+bombs?|toxic\s+gas\s+attacks?|chemical\s+warfare)\b|化学武器|神经毒剂|放射性武器|脏弹|毒气袭击|化学战剂`,
		Weight:   80,
		Category: "weapons",
	},
	{
		Name:     "candidate_sensitive_data_trade",
		Regex:    `(?i)\b(?:buy|sell|leak|publish|trade)\b.{0,80}\b(?:stolen\s+(?:personal\s+)?data|medical\s+records?|identity\s+numbers?|private\s+addresses?|phone\s+(?:numbers?|lists?)|doxxing\s+databases?)\b|(?:购买|出售|交易|泄露|公布).{0,60}(?:社工库|开盒数据|个人信息库|身份证号码|手机号|电话号码|就医记录|病历数据|私人住址)|(?:出售|交易).{0,50}个人隐私`,
		Weight:   75,
		Category: "privacy",
	},
	{
		Name:     "candidate_self_harm_promotion",
		Regex:    `(?i)\b(?:encourage|persuade|pressure|convince|promote)\b.{0,80}\b(?:suicide|self[-\s]harm|anorexia|bulimia|eating\s+disorders?|disordered\s+eating)\b|\b(?:hide|conceal)\b.{0,60}\b(?:starvation|eating\s+disorders?|self[-\s]harm)\b|(?:鼓励|诱导|逼迫|怂恿|劝说).{0,60}(?:自杀|自残|厌食|催吐|进食障碍)|(?:隐藏|掩盖).{0,40}(?:自残行为|进食障碍|厌食症)`,
		Weight:   80,
		Category: "self_harm",
	},
	{
		Name:     "candidate_copyright_circumvention",
		Regex:    `(?i)\b(?:bypass|remove|crack|defeat|strip)\b.{0,80}\b(?:drm|digital\s+rights\s+management|copy[-\s]protection|copyright\s+protection)\b|(?:破解|绕过|移除|去除|解除).{0,60}(?:DRM|数字版权管理|复制保护|版权保护)`,
		Weight:   70,
		Category: "copyright",
	},
	{
		Name:     "candidate_biometric_privacy_abuse",
		Regex:    `(?i)\b(?:identify|track|recognize)\b.{0,60}\b(?:strangers|protesters|people\s+in\s+public)\b.{0,80}\b(?:faces?|facial\s+recognition|surveillance\s+cameras?)\b|\b(?:build|create|collect)\b.{0,80}\b(?:facial\s+recognition|face)\s+databases?\b.{0,60}\bwithout\s+consent\b|(?:识别|追踪|监控).{0,40}(?:陌生人|抗议者|公共场所人员).{0,60}(?:人脸|面部|摄像头)|(?:未经同意|未经授权).{0,40}(?:建立|收集|创建).{0,40}人脸(?:识别)?数据库`,
		Weight:   75,
		Category: "biometric",
	},
}, cyberIntentPatternConfigs...)
