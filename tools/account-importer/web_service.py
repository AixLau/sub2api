"""Web UI for importing and monitoring OpenAI OAuth accounts in Sub2API."""

from __future__ import annotations

import argparse
import getpass
import os
import secrets
from contextlib import asynccontextmanager
from functools import partial
from pathlib import Path

import uvicorn
from fastapi import FastAPI, File, Form, Header, HTTPException, UploadFile
from fastapi.exceptions import RequestValidationError
from fastapi.responses import FileResponse, HTMLResponse, JSONResponse
from pydantic import BaseModel, Field

from import_payload import MAX_UPLOAD_BYTES, parse_bundle
from job_manager import JobManager
from local_relogin import DEFAULT_SOURCE_DIR, relogin_payload
from sub2api_client import AdminAPIError, Sub2APIClient

STATIC = Path(__file__).resolve().parent / "static"


class AccountInput(BaseModel):
    account_line: str = Field(min_length=8, max_length=4096)
    group_ids: list[int] = Field(default_factory=list, max_length=20)
    plugin_id: int | None = Field(default=None, gt=0, strict=True)


async def _bundle(file: UploadFile) -> dict:
    raw = await file.read(MAX_UPLOAD_BYTES + 1)
    await file.close()
    return parse_bundle(raw)


def create_app(client: Sub2APIClient, *, login=relogin_payload, watch_seconds=1200, poll_seconds=30) -> FastAPI:
    manager = JobManager(client, login=login, watch_seconds=watch_seconds, poll_seconds=poll_seconds)
    csrf_token = secrets.token_urlsafe(32)

    @asynccontextmanager
    async def lifespan(_app: FastAPI):
        try:
            yield
        finally:
            await manager.close()

    app = FastAPI(title="Sub2API 导入监控", docs_url=None, redoc_url=None, openapi_url=None, lifespan=lifespan)
    app.state.manager = manager

    @app.exception_handler(RequestValidationError)
    async def validation_error(_request, _error):
        return JSONResponse(status_code=422, content={"detail": "请求参数无效"})

    @app.middleware("http")
    async def no_store(request, call_next):
        response = await call_next(request)
        response.headers["Cache-Control"] = "no-store"
        response.headers["X-Content-Type-Options"] = "nosniff"
        response.headers["X-Frame-Options"] = "DENY"
        response.headers["Referrer-Policy"] = "no-referrer"
        response.headers["Content-Security-Policy"] = "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'"
        return response

    def require_csrf(x_csrf_token: str | None) -> None:
        if not x_csrf_token or not secrets.compare_digest(x_csrf_token, csrf_token):
            raise HTTPException(status_code=403, detail="请求验证失败，请刷新页面")

    @app.get("/healthz")
    async def health():
        return {"status": "ok"}

    @app.get("/", response_class=HTMLResponse)
    async def index():
        html = (STATIC / "index.html").read_text(encoding="utf-8")
        return html.replace("__CSRF_TOKEN__", csrf_token)

    @app.get("/app.css")
    async def css():
        return FileResponse(STATIC / "app.css", media_type="text/css")

    @app.get("/app.js")
    async def javascript():
        return FileResponse(STATIC / "app.js", media_type="text/javascript")

    @app.get("/api/config")
    async def config():
        try:
            groups = await client.list_groups()
        except AdminAPIError:
            groups = []
        try:
            accounts = await manager.profile_accounts()
        except AdminAPIError:
            accounts = []
        return {"base_url": client.base_url, "groups": groups, "accounts": accounts}

    @app.get("/api/jobs")
    async def jobs():
        return manager.list_jobs()

    @app.get("/api/plugins")
    async def plugins():
        try:
            return await client.list_enabled_plugins()
        except AdminAPIError:
            raise HTTPException(status_code=502, detail="无法读取 Sub2API 插件列表，请检查管理员权限后刷新") from None

    @app.post("/api/jobs/reference/watch", status_code=202)
    async def watch_reference(account_id: int, x_csrf_token: str | None = Header(None)):
        require_csrf(x_csrf_token)
        try:
            return await manager.watch_reference(account_id)
        except ValueError as exc:
            raise HTTPException(status_code=400, detail=str(exc)) from None
        except AdminAPIError:
            raise HTTPException(status_code=502, detail="Sub2API 管理员接口不可用") from None

    @app.post("/api/import/file", status_code=202)
    async def import_file(
        file: UploadFile = File(...), group_id: int | None = Form(None),
        profile_account_id: int | None = Form(None),
        plugin_id: int | None = Form(None),
        x_csrf_token: str | None = Header(None),
    ):
        require_csrf(x_csrf_token)
        if group_id is not None and group_id <= 0:
            raise HTTPException(status_code=400, detail="分组 ID 无效")
        try:
            payload = await _bundle(file)
            return await manager.import_file(payload, [group_id] if group_id else None, profile_account_id, plugin_id)
        except ValueError as exc:
            raise HTTPException(status_code=400, detail=str(exc)) from None
        except AdminAPIError:
            raise HTTPException(status_code=502, detail="Sub2API 管理员接口不可用") from None

    @app.post("/api/import/account", status_code=202)
    async def import_account(body: AccountInput, profile_account_id: int | None = None, x_csrf_token: str | None = Header(None)):
        require_csrf(x_csrf_token)
        try:
            return await manager.import_credentials(body.account_line, body.group_ids, profile_account_id, body.plugin_id)
        except ValueError as exc:
            raise HTTPException(status_code=400, detail=str(exc)) from None
        except AdminAPIError:
            raise HTTPException(status_code=502, detail="Sub2API 管理员接口不可用") from None

    @app.post("/api/jobs/{job_id}/reauthorize")
    async def reauthorize(job_id: str, file: UploadFile = File(...), x_csrf_token: str | None = Header(None)):
        require_csrf(x_csrf_token)
        try:
            return await manager.reauthorize_file(job_id, await _bundle(file))
        except ValueError as exc:
            raise HTTPException(status_code=400, detail=str(exc)) from None

    return app


def main() -> None:
    parser = argparse.ArgumentParser(description="本机 Sub2API OAuth 导入与 20 分钟监控服务")
    parser.add_argument("--port", type=int, default=8765)
    parser.add_argument("--source-dir", type=Path, default=DEFAULT_SOURCE_DIR)
    args = parser.parse_args()
    key_file = os.getenv("SUB2API_ADMIN_API_KEY_FILE")
    key = Path(key_file).read_text().strip() if key_file else os.getenv("SUB2API_ADMIN_API_KEY", "")
    if not key:
        key = getpass.getpass("Sub2API 管理员密钥: ")
    base_url = os.getenv("SUB2API_BASE_URL", "").strip()
    if not base_url:
        parser.error("必须设置 SUB2API_BASE_URL")
    client = Sub2APIClient(base_url, key)
    login = partial(relogin_payload, source_dir=args.source_dir)
    uvicorn.run(create_app(client, login=login), host="127.0.0.1", port=args.port, access_log=False, proxy_headers=False)


if __name__ == "__main__":
    main()
