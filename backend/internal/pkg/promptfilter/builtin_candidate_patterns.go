package promptfilter

// Candidate rules are deliberately non-strict and non-operational. They widen
// local recall for candidate_only, but only a downstream reviewer can decide
// whether the request is allowed or blocked.
const candidateSourceRevision = "local-candidate-recall@2026-07-22"

var candidatePatternConfigs = []PatternConfig{
	{
		Name:     "candidate_unauthorized_access",
		Regex:    `(?i)\b(?:break\s+into|hack\s+into|gain\s+unauthori[sz]ed\s+access\s+to|access\s+without\s+(?:permission|authorization)|bypass\s+(?:the\s+)?(?:login|authentication|access\s+control))\b.{0,100}\b(?:account|email|server|system|network|database|device|admin|portal)\b|(?:入侵|黑进|未授权访问|未经授权访问|绕过(?:登录|认证|鉴权|访问控制)).{0,80}(?:账号|邮箱|服务器|系统|网络|数据库|设备|后台)`,
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
		Regex:    `(?i)\b(?:doxx?|deanonymi[sz]e|unmask|track\s+down|find|locate|reveal|publish)\b.{0,100}\b(?:someone(?:'s)?|a\s+private\s+person(?:'s)?|this\s+person(?:'s)?|their)\b.{0,60}\b(?:home\s+address|phone\s+number|current\s+location|real\s+identity|private\s+records?)\b|(?:人肉|开盒|去匿名|查找|定位|曝光|公布).{0,80}(?:某人|个人|这个人|他人).{0,50}(?:家庭住址|手机号|当前位置|真实身份|隐私记录)`,
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
}
