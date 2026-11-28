var __spreadArray = (this && this.__spreadArray) || function (to, from, pack) {
    if (pack || arguments.length === 2) for (var i = 0, l = from.length, ar; i < l; i++) {
        if (ar || !(i in from)) {
            if (!ar) ar = Array.prototype.slice.call(from, 0, i);
            ar[i] = from[i];
        }
    }
    return to.concat(ar || Array.prototype.slice.call(from));
};
/**
 * 公开页面 SEO 数据的单一来源。
 *
 * 同时被两处使用：
 * - 浏览器端 `utils/landingSeo.ts`：页面挂载后同步 head；
 * - 构建期 `vite.config.ts` 的 seoPrerender 插件：为每个公开路由生成独立 HTML
 *   （带专属 title / description / canonical / JSON-LD 和正文），供不执行 JS 的
 *   搜索引擎与 AI 爬虫读取。正文在 Vue 挂载时被替换，用户可见页面不变。
 *
 * 本文件不得访问 window / document，且只用相对路径导入（构建期由 Vite SSR 加载）。
 */
import { brandName, capabilities, faqItems, publicPageLinks } from '../data/landing';
export var SEO_SITE_URL = 'https://aixlau.me';
export var INDEXABLE_ROBOTS = 'index,follow,max-image-preview:large,max-snippet:-1,max-video-preview:-1';
export var NOINDEX_ROBOTS = 'noindex,nofollow,noarchive';
var HOME_TITLE = '星链AI｜Codex 接入与 GPT API 中转站';
var HOME_DESCRIPTION = '星链AI 提供 Codex 接入与 GPT API 中转服务，统一兼容 OpenAI、Claude、Gemini API，支持 API Key 管理、Codex / Claude Code 配置和调用用量查看。';
var statusItems = [
    { label: '服务状态', detail: '查看当前服务可用情况' },
    { label: '调用记录', detail: '查询延迟、Token、费用与错误' },
    { label: '额度管理', detail: '查看余额、订阅与 API Key 用量' },
];
function escapeHtml(value) {
    return value.replace(/[&<>"']/g, function (character) { return ({
        '&': '&amp;',
        '<': '&lt;',
        '>': '&gt;',
        '"': '&quot;',
        "'": '&#39;',
    })[character] || character; });
}
function renderNav() {
    var links = __spreadArray(__spreadArray([{ label: '首页', href: '/' }], publicPageLinks, true), [{ label: '接入教程', href: '/docs/install/' }], false);
    return "<nav aria-label=\"\u4E3B\u5BFC\u822A\"><ul>".concat(links
        .map(function (link) { return "<li><a href=\"".concat(escapeHtml(link.href), "\">").concat(escapeHtml(link.label), "</a></li>"); })
        .join(''), "</ul></nav>");
}
function renderLayout(main) {
    return ("<div class=\"seo-fallback\"><header><a href=\"/\"><strong>".concat(escapeHtml(brandName), "</strong></a>").concat(renderNav(), "</header>") +
        "<main>".concat(main, "</main>") +
        "<footer><p><a href=\"/register\">\u6CE8\u518C</a> \u00B7 <a href=\"/login\">\u767B\u5F55</a> \u00B7 <a href=\"/docs/install/\">Codex / Claude Code \u63A5\u5165\u6559\u7A0B</a></p></footer></div>");
}
function renderHomeBody() {
    return renderLayout("<h1>Codex \u4E0E\u6A21\u578B API \u63A5\u5165\uFF0C\u50CF\u5149\u4E00\u6837\u81EA\u7136\u3002</h1>" +
        "<p>\u661F\u94FEAI \u63D0\u4F9B\u7A33\u5B9A\u7684 Codex \u63A5\u5165\u4E0E GPT API \u4E2D\u8F6C\u670D\u52A1\uFF0C\u7EDF\u4E00\u517C\u5BB9 OpenAI\u3001Claude\u3001Gemini \u7B49\u4E3B\u6D41\u6A21\u578B\uFF0C\u51CF\u5C11\u590D\u6742\u914D\u7F6E\uFF0C\u8BA9\u56E2\u961F\u628A\u6CE8\u610F\u529B\u7559\u7ED9\u4EA7\u54C1\u4F53\u9A8C\u3002</p>" +
        "<p><a href=\"/login\">\u5F00\u59CB\u63A5\u5165</a> \u00B7 <a href=\"/docs/install/\">\u67E5\u770B\u63A5\u5165\u6559\u7A0B</a></p>" +
        "<section><h2>\u4E86\u89E3\u661F\u94FEAI</h2><ul>".concat(publicPageLinks
            .map(function (link) {
            return "<li><a href=\"".concat(escapeHtml(link.href), "\">").concat(escapeHtml(link.label), "</a>\uFF1A").concat(escapeHtml(link.description), "</li>");
        })
            .join(''), "</ul></section>"));
}
function renderServicesBody() {
    return renderLayout("<h1>\u590D\u6742\u80FD\u529B\uFF0C\u4FDD\u6301\u7B80\u5355\u3002</h1><p>\u8986\u76D6\u6A21\u578B\u4F7F\u7528\u3001\u7528\u91CF\u67E5\u770B\u548C\u8D26\u53F7\u7BA1\u7406\u7B49\u5E38\u7528\u9700\u6C42\u3002</p>" +
        capabilities
            .map(function (item) {
            return "<section><h2>".concat(escapeHtml(item.title), "</h2><p>").concat(escapeHtml(item.description), "</p><p>").concat(escapeHtml(item.detail), "</p></section>");
        })
            .join(''));
}
function renderStatusBody() {
    return renderLayout("<h1>\u72B6\u6001\u4E0E\u7528\u91CF\uFF0C\u90FD\u6709\u8FF9\u53EF\u5FAA\u3002</h1><p>\u767B\u5F55\u540E\u53EF\u67E5\u770B\u670D\u52A1\u72B6\u6001\u3001\u8BF7\u6C42\u660E\u7EC6\u548C\u8D39\u7528\u4FE1\u606F\u3002</p>" +
        "<ul>".concat(statusItems
            .map(function (item) { return "<li><strong>".concat(escapeHtml(item.label), "</strong>\uFF1A").concat(escapeHtml(item.detail), "</li>"); })
            .join(''), "</ul>"));
}
function renderModelMarketBody() {
    return renderLayout("<h1>\u6A21\u578B\u4E0E\u4EF7\u683C\uFF0C\u63A5\u5165\u524D\u5C31\u770B\u6E05\u3002</h1><p>\u67E5\u770B\u5F53\u524D\u53EF\u7528\u6A21\u578B\u3001\u8BA1\u8D39\u65B9\u5F0F\u4E0E\u516C\u5F00\u4EF7\u683C\u3002</p>" +
        "<p>\u661F\u94FEAI \u7EDF\u4E00\u63A5\u5165 OpenAI GPT\u3001Anthropic Claude\u3001Google Gemini \u7B49\u6A21\u578B API\uFF0C\u4EF7\u683C\u6309\u8F93\u5165\u3001\u8F93\u51FA\u4E0E\u7F13\u5B58 Token \u8BA1\u8D39\uFF1B\u6700\u65B0\u53EF\u7528\u6A21\u578B\u4E0E\u4EF7\u683C\u4EE5\u672C\u9875\u5B9E\u65F6\u6570\u636E\u548C\u63A7\u5236\u53F0\u4E3A\u51C6\u3002</p>");
}
function renderFaqBody() {
    return renderLayout("<h1>\u63A5\u5165\u524D\uFF0C\u5148\u4E86\u89E3\u8FD9\u4E9B\u3002</h1><p>\u8FD9\u91CC\u8BF4\u660E\u4EA7\u54C1\u80FD\u529B\u548C\u670D\u52A1\u8FB9\u754C\uFF1B\u5177\u4F53\u6A21\u578B\u3001\u5957\u9910\u4E0E\u4EF7\u683C\u4EE5\u63A7\u5236\u53F0\u4E3A\u51C6\u3002</p>" +
        faqItems
            .map(function (item) { return "<section><h2>".concat(escapeHtml(item.question), "</h2><p>").concat(escapeHtml(item.answer), "</p></section>"); })
            .join(''));
}
export function buildHomeSchema(siteUrl) {
    return {
        '@context': 'https://schema.org',
        '@graph': [
            {
                '@type': 'Organization',
                '@id': "".concat(siteUrl, "/#organization"),
                name: '星链AI',
                alternateName: ['星链 AI', 'AIXLAU'],
                url: "".concat(siteUrl, "/"),
                logo: "".concat(siteUrl, "/logo.png"),
            },
            {
                '@type': 'WebSite',
                '@id': "".concat(siteUrl, "/#website"),
                url: "".concat(siteUrl, "/"),
                name: '星链AI',
                alternateName: '星链 AI',
                description: '面向开发者的 Codex 接入与 GPT API 中转站，提供 OpenAI、Claude、Gemini API 统一中转与兼容接入',
                inLanguage: 'zh-CN',
                publisher: { '@id': "".concat(siteUrl, "/#organization") },
            },
            {
                '@type': 'SoftwareApplication',
                '@id': "".concat(siteUrl, "/#application"),
                name: '星链AI',
                alternateName: '星链 AI',
                applicationCategory: 'DeveloperApplication',
                operatingSystem: 'Web',
                url: "".concat(siteUrl, "/"),
                description: '面向开发者的 GPT API 中转站，支持 OpenAI API、Claude API、Gemini API 和 Codex 的统一中转接入。',
                keywords: 'GPT API 中转站,OpenAI API 中转,Claude API 中转,Gemini API 中转,Codex 中转站,AI API 中转站',
                featureList: [
                    'GPT API 中转站',
                    'OpenAI API 中转',
                    'Claude API 中转',
                    'Gemini API 中转',
                    'Codex / Claude Code 配置',
                    'API Key 管理',
                ],
            },
        ],
    };
}
function buildFaqSchema() {
    return {
        '@context': 'https://schema.org',
        '@type': 'FAQPage',
        mainEntity: faqItems.map(function (item) { return ({
            '@type': 'Question',
            name: item.question,
            acceptedAnswer: { '@type': 'Answer', text: item.answer },
        }); }),
    };
}
function authPage(title) {
    return { title: title, description: '星链AI 账号入口。', robots: NOINDEX_ROBOTS };
}
export var seoPages = {
    '/': {
        title: HOME_TITLE,
        description: HOME_DESCRIPTION,
        robots: INDEXABLE_ROBOTS,
        body: renderHomeBody,
        schema: buildHomeSchema,
    },
    '/model-market': {
        title: 'GPT、Claude、Gemini API 模型与价格 - 星链AI',
        description: '查看星链AI GPT API 中转站当前公开的 OpenAI、Claude、Gemini 等可售模型、API 标识与基础 Token 价格。',
        robots: INDEXABLE_ROBOTS,
        body: renderModelMarketBody,
    },
    '/services': {
        title: 'AI API 中转站服务能力 - 星链AI',
        description: '了解星链AI 的 GPT、OpenAI、Claude、Gemini API 中转接入、用量查看与 API Key 管理能力。',
        robots: INDEXABLE_ROBOTS,
        body: renderServicesBody,
    },
    '/service-status': {
        title: 'API 中转服务状态与用量 - 星链AI',
        description: '了解星链AI API 中转服务的可用状态、调用记录、Token 费用与 API Key 额度查看能力。',
        robots: INDEXABLE_ROBOTS,
        body: renderStatusBody,
    },
    '/faq': {
        title: 'GPT API 中转站常见问题 - 星链AI',
        description: '了解 GPT API 中转站的适用场景，以及星链AI 的 OpenAI、Claude、Gemini、Codex 接入、费用和调用明细。',
        robots: INDEXABLE_ROBOTS,
        body: renderFaqBody,
        schema: buildFaqSchema,
    },
    '/login': authPage('登录 - 星链AI'),
    '/register': authPage('注册 - 星链AI'),
    '/forgot-password': authPage('找回密码 - 星链AI'),
    '/reset-password': authPage('重置密码 - 星链AI'),
};
/** 未登记路径（含首页别名）一律按首页处理，与原 landingSeo 行为一致。 */
export function getSeoPage(pathname) {
    var page = seoPages[pathname];
    return page ? { path: pathname, page: page } : { path: '/', page: seoPages['/'] };
}
export var SEO_FALLBACK_STYLE = '.seo-fallback{max-width:960px;margin:0 auto;padding:24px 16px;font-family:system-ui,-apple-system,"PingFang SC","Microsoft YaHei",sans-serif;line-height:1.7;color:#1f2937}' +
    '.seo-fallback a{color:#2563eb}.seo-fallback nav ul{display:flex;flex-wrap:wrap;gap:12px;list-style:none;padding:0}';
