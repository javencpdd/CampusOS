"""Local HTTP client shared by disposable G0 acceptance drills."""

import json
import urllib.error
import urllib.request
from http.cookies import SimpleCookie


class Client:
    def __init__(self, base):
        self.base = base
        self.token = ""
        self.cookies = {}
        # The drill is strictly local; ignore host proxy settings and redirects.
        self.opener = urllib.request.build_opener(
            urllib.request.ProxyHandler({}), NoRedirect()
        )

    def request(self, method, path, body=None, csrf=False):
        status, _, raw = self.request_bytes(method, path, body, csrf)
        return status, json.loads(raw)

    def request_bytes(self, method, path, body=None, csrf=False):
        """Return status, headers and bytes for JSON errors or file downloads."""
        headers = {"Content-Type": "application/json"}
        if self.token:
            headers["Authorization"] = "Bearer " + self.token
        if path.startswith("/api/v1/auth/"):
            headers["Cookie"] = "; ".join(
                f"{name}={cookie.value}" for name, cookie in self.cookies.items()
            )
        if csrf:
            headers["X-CSRF-Token"] = self.cookies["campusos_csrf"].value
        request = urllib.request.Request(
            self.base + path,
            data=None if body is None else json.dumps(body).encode(),
            headers=headers,
            method=method,
        )
        try:
            response = self.opener.open(request, timeout=5)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            raw = response.read()
            for value in response.headers.get_all("Set-Cookie", []):
                parsed = SimpleCookie()
                parsed.load(value)
                self.cookies.update(parsed)
            return response.status, response.headers, raw


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def require(condition, message):
    if not condition:
        raise SystemExit(message)


