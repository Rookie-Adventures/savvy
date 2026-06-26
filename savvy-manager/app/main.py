from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from .database import Base, engine
from .routers import health, users, instances, workspace
from .scanner import start_scanner, stop_scanner

app = FastAPI(
    title="Savvy Manager",
    description="Manages Hermes cloud workspace instances for Savvy Agent",
    version="0.1.0",
)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

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
