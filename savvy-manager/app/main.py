import json
from fastapi import FastAPI, Request, Response
from fastapi.middleware.cors import CORSMiddleware
from .database import Base, engine
from .routers import health, users, instances, workspace
from .scanner import start_scanner, stop_scanner

app = FastAPI(
    title="Savvy Manager",
    description="Manages Hermes cloud workspace instances for Savvy Agent",
    version="0.1.0",
)

# Because Savvy Manager is a private HMAC-protected API and is only called by
# the new-api backend, we restrict/disable wide CORS origins in production.
app.add_middleware(
    CORSMiddleware,
    allow_origins=["http://localhost", "http://127.0.0.1"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)


@app.middleware("http")
async def envelope_response_middleware(request: Request, call_next):
    response = await call_next(request)

    # Only wrap internal requests to match new-api expected HermesManagerResponse format:
    # {"success": bool, "message": str, "data": Any}
    if request.url.path.startswith("/internal/"):
        # We need to bypass this wrapper for the workspace nginx auth subrequest (GET /internal/workspace/validate)
        # because Nginx auth_request expects a clean status 200 without custom body content,
        # but even if we wrap it, Nginx ignores body. However, keeping it clean is safer.
        if request.url.path == "/internal/workspace/validate":
            return response

        # Read original response body
        body_chunks = []
        async for chunk in response.body_iterator:
            body_chunks.append(chunk)
        body_bytes = b"".join(body_chunks)

        # Parse original json if possible
        try:
            original_data = json.loads(body_bytes.decode("utf-8"))
        except Exception:
            original_data = body_bytes.decode("utf-8") if body_bytes else None

        # Build envelope
        if response.status_code == 200:
            enveloped_body = {
                "success": True,
                "message": "",
                "data": original_data
            }
        else:
            # Handle exception/error details
            error_msg = "manager returned failure"
            if isinstance(original_data, dict):
                error_msg = original_data.get("detail", error_msg)
            elif isinstance(original_data, str):
                error_msg = original_data

            enveloped_body = {
                "success": False,
                "message": error_msg,
                "data": None
            }

        # Return new response
        new_headers = dict(response.headers)
        new_headers.pop("content-length", None)  # Remove old length to let Starlette recalculate correctly
        new_content = json.dumps(enveloped_body).encode("utf-8")
        return Response(
            content=new_content,
            status_code=200,  # Map internal errors inside success/failure envelope
            headers=new_headers,
            media_type="application/json"
        )

    return response


app.include_router(health.router)
app.include_router(users.router)
app.include_router(instances.router)
app.include_router(workspace.router)


@app.on_event("startup")
async def startup():
    Base.metadata.create_all(bind=engine)
    start_scanner()


@app.on_event("shutdown")
async def shutdown():
    stop_scanner()
