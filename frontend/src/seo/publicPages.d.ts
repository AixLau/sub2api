export declare const SEO_SITE_URL = "https://aixlau.me";
export declare const INDEXABLE_ROBOTS = "index,follow,max-image-preview:large,max-snippet:-1,max-video-preview:-1";
export declare const NOINDEX_ROBOTS = "noindex,nofollow,noarchive";
export type SeoPage = {
    title: string;
    description: string;
    robots: string;
    /** 构建期预渲染的正文；为空表示只输出 head（如登录页）。 */
    body?: () => string;
    schema?: (siteUrl: string) => Record<string, unknown>;
};
export declare function buildHomeSchema(siteUrl: string): Record<string, unknown>;
export declare const seoPages: Record<string, SeoPage>;
/** 未登记路径（含首页别名）一律按首页处理，与原 landingSeo 行为一致。 */
export declare function getSeoPage(pathname: string): {
    path: string;
    page: SeoPage;
};
export declare const SEO_FALLBACK_STYLE: string;
