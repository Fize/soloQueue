#!/usr/bin/env python3
"""Browser E2E checks for the SoloQueue Web Console.

The script intentionally has no pytest dependency.  It is run with the Python
Playwright package and reports PASS/FAIL/BLOCKED per scenario so it is useful in
minimal CI and in a local regression run.
"""

from __future__ import annotations

import argparse
import os
import sys
import traceback
from dataclasses import dataclass
from pathlib import Path
from typing import Callable

from playwright.sync_api import Browser, BrowserContext, Page, Playwright, sync_playwright


BASE_URL = os.environ.get("E2E_BASE_URL", "http://127.0.0.1:5173")
ARTIFACT_DIR = Path(os.environ.get("E2E_ARTIFACT_DIR", "e2e-artifacts"))


@dataclass
class Result:
    name: str
    status: str
    detail: str = ""


class E2E:
    def __init__(self, browser: Browser, base_url: str):
        self.browser = browser
        self.base_url = base_url.rstrip("/")
        self.results: list[Result] = []
        self.counter = 0

    def context(self, width: int = 1280, height: int = 800) -> BrowserContext:
        return self.browser.new_context(viewport={"width": width, "height": height})

    def run(self, name: str, fn: Callable[[Page], str | None]) -> None:
        self.counter += 1
        context = self.context()
        page = context.new_page()
        console_errors: list[str] = []
        request_failures: list[str] = []
        server_errors: list[str] = []
        page.on("console", lambda msg: console_errors.append(msg.text) if msg.type == "error" else None)
        def on_request_failed(req) -> None:
            failure = req.failure or ""
            # React route changes abort in-flight stats fetches as the old page
            # unmounts. They are expected lifecycle cancellations, not broken
            # network requests; unexpected failures still fail the scenario.
            if "ERR_ABORTED" not in failure and "CANCELED" not in failure.upper():
                request_failures.append(f"{req.method} {req.url}: {failure}")

        page.on("requestfailed", on_request_failed)
        page.on("response", lambda response: server_errors.append(f"{response.status} {response.url}") if response.status >= 500 else None)
        try:
            detail = fn(page) or ""
            evidence = []
            if console_errors:
                evidence.append(f"console errors: {console_errors[:3]}")
            if request_failures:
                evidence.append(f"request failures: {request_failures[:3]}")
            if server_errors:
                evidence.append(f"HTTP 5xx: {server_errors[:3]}")
            if evidence:
                raise AssertionError("; ".join(evidence))
            self.results.append(Result(name, "PASS", detail))
        except AssertionError as exc:
            artifact = ARTIFACT_DIR / f"{self.counter:02d}-{name.replace(' ', '-').lower()}.png"
            ARTIFACT_DIR.mkdir(parents=True, exist_ok=True)
            page.screenshot(path=str(artifact), full_page=True)
            self.results.append(Result(name, "FAIL", f"{exc}; screenshot={artifact}"))
        except Exception as exc:  # keep the remaining matrix running
            artifact = ARTIFACT_DIR / f"{self.counter:02d}-{name.replace(' ', '-').lower()}.png"
            ARTIFACT_DIR.mkdir(parents=True, exist_ok=True)
            page.screenshot(path=str(artifact), full_page=True)
            self.results.append(Result(name, "FAIL", f"{type(exc).__name__}: {exc}; screenshot={artifact}"))
            traceback.print_exc()
        finally:
            context.close()

    def blocked(self, name: str, detail: str) -> None:
        self.results.append(Result(name, "BLOCKED", detail))

    def goto(self, page: Page, route: str = "#/chat") -> None:
        response = page.goto(f"{self.base_url}/{route.lstrip('/')}", wait_until="networkidle")
        assert response and response.ok, f"initial document unavailable: {response.status if response else 'no response'}"
        page.wait_for_timeout(300)

    def dismiss_install_prompt(self, page: Page) -> None:
        button = page.get_by_role("button", name="Not now", exact=True)
        if button.count():
            button.click()


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--base-url", default=BASE_URL)
    args = parser.parse_args()
    suite: E2E | None = None

    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        suite = E2E(browser, args.base_url)

        def shell(page: Page) -> str:
            response = page.goto(f"{suite.base_url}/", wait_until="networkidle")
            assert response and response.ok, "bare root document unavailable"
            page.wait_for_timeout(300)
            assert page.url.endswith("#/chat"), f"bare root did not redirect: {page.url}"
            suite.dismiss_install_prompt(page)
            assert page.title() == "SoloQueue"
            assert page.locator('link[rel="manifest"]').count() == 1
            assert page.get_by_text("New Chat", exact=True).count() >= 1
            assets = page.evaluate(
                """
                async () => {
                  const [manifest, sw] = await Promise.all([
                    fetch('/manifest.webmanifest'),
                    fetch('/sw.js'),
                  ]);
                  return {
                    manifestOk: manifest.ok,
                    manifestType: manifest.headers.get('content-type') || '',
                    swOk: sw.ok,
                    swText: await sw.text(),
                  };
                }
                """
            )
            assert assets["manifestOk"] and "manifest" in assets["manifestType"], assets
            assert assets["swOk"] and "addEventListener" in assets["swText"], assets
            return f"url={page.url}"

        suite.run("root shell and pwa assets", shell)

        def nav(page: Page) -> str:
            suite.goto(page)
            suite.dismiss_install_prompt(page)
            for label, marker in [
                ("Assistant", "Assistant"),
                ("Simulations", "Sandbox Simulations"),
                ("Cron Jobs", "Cron Jobs"),
                ("Usage Statistics", "Usage Statistics"),
            ]:
                page.get_by_role("button", name=label, exact=True).click()
                try:
                    page.locator("main").get_by_text(marker, exact=False).first.wait_for(state="visible", timeout=10000)
                except Exception as exc:
                    raise AssertionError(f"{label} marker missing: {marker!r}; body={page.locator('body').inner_text()[:300]!r}") from exc
                expected_url = {
                    "Assistant": "#/assistant",
                    "Simulations": "#/simulations",
                    "Cron Jobs": "#/cron",
                    "Usage Statistics": "?stats_range=30d",
                }[label]
                assert expected_url in page.url, f"{label} route mismatch: {page.url}"
            page.goto(f"{suite.base_url}/#/settings/general", wait_until="networkidle")
            assert "General Preferences" in page.locator("body").inner_text(), page.locator("body").inner_text()[:300]
            return "assistant, simulations, cron, statistics, settings"

        suite.run("primary navigation and settings", nav)

        def chat_input(page: Page) -> str:
            suite.goto(page)
            box = page.locator("textarea[placeholder='Ask anything...']")
            assert box.is_visible()
            box.fill("E2E draft")
            box.press("Control+A")
            box.press("ArrowLeft")
            assert box.input_value() == "E2E draft"
            return "input accepts text and keyboard navigation without sending"

        suite.run("chat input safe interaction", chat_input)

        def phone(page: Page) -> str:
            page.set_viewport_size({"width": 390, "height": 844})
            suite.goto(page)
            suite.dismiss_install_prompt(page)
            assert page.get_by_title("Design Mode", exact=True).count() == 0
            assert page.locator("textarea[placeholder='Ask anything...']").is_visible()
            page.get_by_label("Collapse sidebar", exact=True).click()
            page.get_by_label("Expand sidebar", exact=True).click()
            page.get_by_role("button", name="Assistant", exact=True).click()
            assert "#/assistant" in page.url, page.url
            return "390x844: phone capability hides Design Mode"

        suite.run("phone responsive boundary", phone)

        def pad_single(page: Page) -> str:
            page.set_viewport_size({"width": 999, "height": 800})
            suite.goto(page)
            suite.dismiss_install_prompt(page)
            button = page.get_by_title("Design Mode", exact=True)
            assert button.is_visible()
            button.click()
            assert page.locator('[data-design-layout="single"]').count() >= 1
            return "999x800: pad single-pane design"

        suite.run("pad single pane responsive boundary", pad_single)

        def pad_split(page: Page) -> str:
            page.set_viewport_size({"width": 1000, "height": 800})
            suite.goto(page)
            suite.dismiss_install_prompt(page)
            page.get_by_title("Design Mode", exact=True).click()
            assert page.locator('[data-design-layout="split"]').count() >= 1
            return "1000x800: pad split-pane design"

        suite.run("pad split pane responsive boundary", pad_split)

        def desktop_split(page: Page) -> str:
            suite.goto(page)
            suite.dismiss_install_prompt(page)
            page.get_by_title("Design Mode", exact=True).click()
            assert page.locator('[data-design-layout="split"]').count() >= 1
            return "1280x800: desktop split-pane design"

        suite.run("desktop responsive boundary", desktop_split)

        def pwa_prompt(page: Page) -> str:
            suite.goto(page)
            page.evaluate("""
                () => {
                  window.__e2ePromptCalled = false;
                  const event = new Event('beforeinstallprompt', {cancelable: true});
                  event.prompt = () => { window.__e2ePromptCalled = true; return Promise.resolve(); };
                  event.userChoice = Promise.resolve({outcome: 'accepted', platform: 'web'});
                  window.dispatchEvent(event);
                }
            """)
            page.get_by_role("button", name="Install", exact=True).click()
            page.wait_for_timeout(100)
            assert page.evaluate("() => window.__e2ePromptCalled") is True
            return "synthetic beforeinstallprompt reaches native install path"

        suite.run("pwa native install prompt", pwa_prompt)

        def pwa_guide(page: Page) -> str:
            suite.goto(page)
            page.get_by_role("button", name="Install Guide", exact=True).click()
            dialog = page.get_by_role("dialog", name="Install SoloQueue")
            assert dialog.is_visible()
            assert "Desktop browser" in dialog.inner_text()
            assert "Mobile browser" in dialog.inner_text()
            dialog.get_by_role("button", name="Done", exact=True).click()
            assert page.get_by_role("dialog").count() == 0
            return "manual desktop/mobile guidance is accessible and dismissible"

        suite.run("pwa manual install guide", pwa_guide)

        def pwa_dismissal(page: Page) -> str:
            suite.goto(page)
            suite.dismiss_install_prompt(page)
            assert page.evaluate("() => localStorage.getItem('soloqueue_pwa_install_dismissed')") == "true"
            page.reload(wait_until="networkidle")
            assert page.get_by_role("region", name="Install SoloQueue").count() == 0
            return "Not now persists across reload in the same browser context"

        suite.run("pwa dismissal persistence", pwa_dismissal)

        # The L1 workspace requires a live delegated-session fixture and must
        # not be faked by an E2E script. Keep it visible in the report.
        suite.blocked("l1 delegated workspace", "no deterministic delegated-session fixture is provided by the local serve command")
        suite.blocked("production service worker registration", "Vite dev mode intentionally does not register sw.js; run this scenario against a production static server")
        browser.close()

    assert suite is not None
    print("E2E RESULTS")
    for result in suite.results:
        suffix = f" — {result.detail}" if result.detail else ""
        print(f"{result.status}: {result.name}{suffix}")
    counts = {status: sum(result.status == status for result in suite.results) for status in ("PASS", "FAIL", "BLOCKED")}
    print(f"TOTAL: {len(suite.results)} PASS={counts['PASS']} FAIL={counts['FAIL']} BLOCKED={counts['BLOCKED']}")
    return 1 if counts["FAIL"] else 0


if __name__ == "__main__":
    raise SystemExit(main())
