#!/usr/bin/env python3
"""Verify a disposable Docker container and volume; Python standard library only."""
import argparse
import json
import subprocess
import time
import urllib.error
import urllib.request
import uuid


def docker(*args):
    return subprocess.check_output(["docker", *args], text=True).strip()


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("image")
    parser.add_argument("--commit", required=True)
    args = parser.parse_args()
    name = "atad-smoke-" + uuid.uuid4().hex[:12]
    volume = name + "-data"
    opener = urllib.request.build_opener(NoRedirect())
    base = ""

    def request(method, path, status, body=None):
        data = None if body is None else json.dumps(body).encode()
        req = urllib.request.Request(base + path, data=data, method=method,
                                     headers={"Content-Type": "application/json"})
        try:
            response = opener.open(req, timeout=5)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            payload = response.read()
            assert response.code == status, (method, path, response.code, payload)
            if status == 204:
                assert not payload
            return payload, response.headers

    def boot():
        nonlocal base
        docker("run", "-d", "--name", name, "-p", "127.0.0.1::8080",
               "-v", volume + ":/data", args.image)
        port = docker("port", name, "8080/tcp").splitlines()[0].rsplit(":", 1)[1]
        base = "http://127.0.0.1:" + port
        for _ in range(60):
            try:
                payload, _ = request("GET", "/health", 200)
                health = json.loads(payload)
                assert health["status"] == "ok" and type(health["uptime_seconds"]) is int
                return
            except (urllib.error.URLError, TimeoutError, ConnectionError):
                time.sleep(0.25)
        raise RuntimeError("container did not become ready")

    docker("volume", "create", volume)
    try:
        boot()
        assert docker("exec", name, "id", "-u") == "10001"
        cli = json.loads(docker("run", "--rm", args.image, "--version"))
        payload, _ = request("GET", "/version", 200)
        info = json.loads(payload)
        assert info == cli and set(info) == {"app", "version", "commit", "built_at"}
        assert info["commit"] == args.commit and info["built_at"] not in ("", "unknown")
        identity, _ = request("GET", "/", 200)
        assert b"Dume Denisa" in identity and b"Project 1" in identity
        request("POST", "/reset", 204)
        link = {"url": "https://example.org/docs", "code": "smoke"}
        payload, _ = request("POST", "/links", 201, link)
        assert json.loads(payload)["id"] == 1
        request("POST", "/links", 409, link)
        _, headers = request("GET", "/smoke", 302)
        assert headers["Location"] == link["url"]
        payload, _ = request("GET", "/links/smoke/stats", 200)
        first = json.loads(payload)
        assert first["visits"] == 1 and first["last_visit_at"] is not None
        payload, _ = request("GET", "/links/smoke/stats", 200)
        assert json.loads(payload) == first
        docker("stop", name)
        docker("rm", name)
        boot()  # Recreate the container with the same named volume.
        payload, _ = request("GET", "/links/smoke/stats", 200)
        assert json.loads(payload) == first
        _, headers = request("GET", "/smoke", 302)
        assert headers["Location"] == link["url"]
        request("DELETE", "/links/smoke", 204)
        request("GET", "/smoke", 404)
        request("POST", "/reset", 204)
        payload, _ = request("GET", "/links", 200)
        assert json.loads(payload) == []
        print("PASS: image identity, non-root execution, API contract, analytics and volume persistence")
    except Exception:
        subprocess.run(["docker", "logs", name], check=False)
        raise
    finally:
        subprocess.run(["docker", "rm", "-f", name], check=False, stdout=subprocess.DEVNULL)
        subprocess.run(["docker", "volume", "rm", volume], check=False, stdout=subprocess.DEVNULL)


if __name__ == "__main__":
    main()
