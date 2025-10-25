#!/usr/bin/env python3
"""Smoke test for service-a trace pipeline."""

from __future__ import annotations

import json
import logging
import subprocess
import sys
import time
from dataclasses import dataclass

logger = logging.getLogger("smoke-test")
logging.basicConfig(level=logging.INFO, format="[%(levelname)s] %(message)s")


def run(cmd: list[str]) -> str:
    """Execute command and return stdout."""
    logger.debug("Running command: %s", " ".join(cmd))
    result = subprocess.run(cmd, capture_output=True, text=True, check=True)
    output = result.stdout.strip()
    logger.debug("Command output: %s", output[:200])
    return output


def load_json(raw: str) -> dict:
    """Parse JSON or exit with error."""
    try:
        return json.loads(raw)
    except json.JSONDecodeError as exc:
        raise SystemExit(f"parse-error: {exc}")


def extract_response_tuple(payload: dict) -> tuple[str, str, str]:
    """Return (order_id, user_id, status) from service response."""
    try:
        return payload["order_id"], payload["user_id"], payload["status"]
    except KeyError as exc:
        raise SystemExit(f"missing key in response: {exc}")


def extract_trace_tuple(payload: dict) -> tuple[str, str, str]:
    """Return tuple from Jaeger trace matching service-a span."""
    for trace in payload.get("data", []):
        processes = trace.get("processes", {})
        for span in trace.get("spans", []):
            proc = processes.get(span.get("processID"))
            if proc and proc.get("serviceName") == "service-a":
                tags = {tag.get("key"): tag.get("value") for tag in span.get("tags", [])}
                if {"order.id", "user.id", "order.status"} <= tags.keys():
                    return tags["order.id"], tags["user.id"], tags["order.status"]
    raise SystemExit("trace-error: required tags not found in Jaeger response")


@dataclass(frozen=True)
class SmokeConfig:
    kubectl: str = "kubectl"
    namespace: str = "demo"
    service_label: str = "app=service-a"
    service_url: str = "http://service-a:8080"
    jaeger_url: str = (
        "http://simplest-query.observability.svc.cluster.local:16686/api/traces"
        "?service=service-a&limit=1"
    )

    def pod_name(self) -> str:
        pods_json = load_json(
            run(
                [
                    self.kubectl,
                    "get",
                    "pods",
                    "-n",
                    self.namespace,
                    "-l",
                    self.service_label,
                    "-o",
                    "json",
                ]
            )
        )
        items = pods_json.get("items", [])
        if not items:
            raise SystemExit("no service-a pods found")
        return items[0]["metadata"]["name"]

    def fetch_response(self, pod: str) -> dict:
        raw = run(
            [
                self.kubectl,
                "exec",
                "-n",
                self.namespace,
                pod,
                "--",
                "wget",
                "-qO-",
                self.service_url,
            ]
        )
        logger.info("Response: %s", raw)
        return load_json(raw)

    def fetch_trace(self, pod: str, attempts: int = 5, delay: float = 2.0) -> dict:
        for attempt in range(1, attempts + 1):
            raw = run(
                [
                    self.kubectl,
                    "exec",
                    "-n",
                    self.namespace,
                    pod,
                    "--",
                    "wget",
                    "-qO-",
                    self.jaeger_url,
                ]
            )
            payload = load_json(raw)
            if payload.get("data"):
                logger.info("Jaeger trace snippet: %s", raw[:200])
                return payload
            logger.debug("Trace data empty (attempt %s/%s)", attempt, attempts)
            time.sleep(delay)
        logger.info("Jaeger trace snippet: %s", raw[:200])
        return payload


def main() -> None:
    cfg = SmokeConfig(kubectl=sys.argv[1] if len(sys.argv) > 1 else "kubectl")
    pod = cfg.pod_name()

    response_payload = cfg.fetch_response(pod)
    response_tuple = extract_response_tuple(response_payload)
    logger.info("Response tuple: %s", response_tuple)

    trace_payload = cfg.fetch_trace(pod)
    trace_tuple = extract_trace_tuple(trace_payload)
    logger.info("Trace tuple: %s", trace_tuple)

    if response_tuple != trace_tuple:
        raise SystemExit(
            f"trace mismatch: response {response_tuple} != trace {trace_tuple}"
        )

    logger.info("✔ Trace pipeline OK")


if __name__ == "__main__":
    main()
