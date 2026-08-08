"""Thin HTTP client for the Oracle, shared by both example schedulers.

Deliberately minimal: this is the "plumbing" half of a scheduler client
(register/login, poll state, submit assignments), kept separate from
scheduling strategy so the two example scripts can focus on the interesting
part — the decision logic — without duplicating request handling.
"""

from __future__ import annotations

import os
from typing import Any

import requests

BASE_URL = os.environ.get("ORACLE_BASE_URL", "http://localhost:8080")


class OracleError(RuntimeError):
    """Raised when the Oracle returns a non-2xx response."""


def _request(method: str, path: str, token: str | None = None, json_body: Any = None) -> Any:
    headers = {"Authorization": f"Bearer {token}"} if token else {}
    resp = requests.request(method, BASE_URL + path, json=json_body, headers=headers, timeout=30)
    if not resp.ok:
        detail = resp.text
        try:
            detail = resp.json().get("detail", resp.text)
        except ValueError:
            pass
        raise OracleError(f"{method} {path} -> {resp.status_code}: {detail}")
    if resp.status_code == 204 or not resp.content:
        return None
    return resp.json()


def login_or_register(email: str, nuid: str) -> str:
    """Logs in if the account already exists, otherwise registers it. Returns a bearer token."""
    try:
        return _request("POST", "/login", json_body={"email": email, "nuid": nuid})["token"]
    except OracleError:
        return _request("POST", "/register", json_body={"email": email, "nuid": nuid})["token"]


def create_expedition(token: str) -> dict:
    return _request("POST", "/expedition", token=token)


def get_expedition(token: str, expedition_id: str) -> dict:
    return _request("GET", f"/expedition/{expedition_id}", token=token)


def submit_cycle(token: str, expedition_id: str, assignments: list[dict]) -> dict:
    return _request("POST", f"/cycle/{expedition_id}/schedule", token=token, json_body=assignments)


def history(token: str) -> list[dict]:
    return _request("GET", "/me/expeditions", token=token)
