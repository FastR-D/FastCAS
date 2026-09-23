"""Optional FastAPI transport for signed FastCAS notifications.

Importing the main fastcas package never imports FastAPI. Applications retain
their own routes, local accounts, transactions and authorization policy.
"""

from __future__ import annotations

import inspect
from typing import Callable
from urllib.parse import parse_qs

from fastapi import HTTPException, Request
from starlette.concurrency import run_in_threadpool

from .client import AsyncFastCAS, FastCAS, FastCASError

MAX_NOTIFICATION_BYTES = 65536


async def _body(request: Request, content_type: str) -> str:
    if request.headers.get("content-type", "").split(";", 1)[0].strip().lower() != content_type:
        raise HTTPException(415, "signed FastCAS notification required")
    body = bytearray()
    async for chunk in request.stream():
        body.extend(chunk)
        if len(body) > MAX_NOTIFICATION_BYTES:
            raise HTTPException(413, "FastCAS notification too large")
    try:
        return body.decode("ascii")
    except UnicodeDecodeError:
        raise HTTPException(400, "invalid FastCAS notification") from None


async def receive_event(request: Request, sdk: FastCAS | AsyncFastCAS, apply: Callable) -> None:
    """Verify an application/jwt event, then await its local transaction.

    apply must atomically deduplicate event.id and update only FastCAS-source
    sessions. A failed apply returns 503 so the FastCAS outbox retries.
    """
    token = await _body(request, "application/jwt")
    started = False
    try:
        if isinstance(sdk, AsyncFastCAS):
            async def async_apply(event):
                nonlocal started
                started = True
                result = apply(event)
                if inspect.isawaitable(result):
                    await result
            await sdk.handle_notification(token, async_apply)
        else:
            if inspect.iscoroutinefunction(apply):
                raise TypeError("synchronous FastCAS needs a synchronous apply callback")
            def sync_apply(event):
                nonlocal started
                started = True
                result = apply(event)
                if inspect.isawaitable(result):
                    raise TypeError("synchronous FastCAS apply callback returned an awaitable")
            await run_in_threadpool(sdk.handle_notification, token, sync_apply)
    except FastCASError:
        raise HTTPException(503 if started else 401, "FastCAS notification was not committed" if started else "invalid FastCAS notification") from None
    except Exception:
        raise HTTPException(503, "FastCAS notification was not committed") from None


async def receive_logout(request: Request, sdk: FastCAS | AsyncFastCAS, apply: Callable) -> None:
    """Verify a back-channel logout and commit replay ID plus session changes.

    apply receives {id, subject, sid, expires_at} and must deduplicate the ID
    and revoke only matching FastCAS-source sessions in one local transaction.
    """
    raw = await _body(request, "application/x-www-form-urlencoded")
    try:
        fields = parse_qs(raw, strict_parsing=True, keep_blank_values=True)
        if set(fields) != {"logout_token"} or len(fields["logout_token"]) != 1 or not fields["logout_token"][0]:
            raise ValueError("invalid logout form")
        token = fields["logout_token"][0]
    except ValueError:
        raise HTTPException(400, "invalid FastCAS logout form") from None
    try:
        notice = await sdk.verify_logout(token) if isinstance(sdk, AsyncFastCAS) else await run_in_threadpool(sdk.verify_logout, token)
    except FastCASError:
        raise HTTPException(401, "invalid FastCAS logout token") from None
    except Exception:
        raise HTTPException(503, "FastCAS logout verification unavailable") from None
    try:
        if inspect.iscoroutinefunction(apply):
            await apply(notice)
        else:
            result = await run_in_threadpool(apply, notice)
            if inspect.isawaitable(result):
                await result
    except Exception:
        raise HTTPException(503, "FastCAS logout was not committed") from None
