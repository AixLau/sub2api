-- 243_opencode_go_platform.sql 在 242 之后执行，又把 242 删除的两个平台 CHECK
-- 约束加了回来，且白名单不含之后登记的平台（如 typesafe / cline / command_code），
-- 导致这些平台的配额与组合路由写入被数据库拒绝。243 已发布、受 checksum 保护
-- 不能修改，这里再次删除，恢复 242 的设计：平台合法性由应用层平台清单
-- （internal/domain/platforms.go）统一校验。
--
-- channel_monitors / channel_monitor_request_templates 的 provider CHECK 表示
-- 渠道监控已实现的探测能力，不属于平台清单，保留不动（同 242）。
--
-- DROP ... IF EXISTS 保证可重入。

ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;

ALTER TABLE composite_model_routes
    DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;
