"""End-to-end test of the offline mode (see the offline plan).

Run after smoke.py on the same instance: changes made without network are
shown at once, survive a reload, and reach the server when the connection
returns.
"""
import json
import os
import sys
from playwright.sync_api import sync_playwright, expect

BASE = os.environ["BASE"]
OUT = os.environ.get("OUT", ".")
errors = []
offline = False  # failed requests are expected while the network is cut
pages = []  # to take a screenshot when the scenario fails


def watch(page):
    def on_console(m):
        if m.type == "error" and "ERR_INTERNET_DISCONNECTED" not in m.text:
            errors.append(f"console error: {m.text}")

    page.on("console", on_console)
    page.on("pageerror", lambda e: errors.append(f"page error: {e}"))


def go(page, key):
    """Switch tab with the keyboard, leaving any focused field first."""
    page.keyboard.press("Escape")
    page.keyboard.press(key)


def add_recipe(page, title, ingredients):
    go(page, "2")
    page.get_by_role("button", name="New recipe").click()
    page.fill(".modal.recipe input", title)
    page.fill(".modal.recipe textarea >> nth=0", "\n".join(ingredients))
    page.get_by_role("button", name="Save").click()
    expect(page.locator(".modal.recipe")).to_have_count(0)


def plan_slot(page, index, text):
    """Choose in the index-th slot (0 = today's lunch)."""
    go(page, "1")
    page.locator(".slot").nth(index).click()
    page.fill(".picker-input", text)
    page.keyboard.press("Enter")
    expect(page.locator(".modal-overlay")).to_have_count(0)


def scenario(browser):
    global offline
    ctx = browser.new_context(viewport={"width": 1100, "height": 760}, timezone_id="Europe/Paris")
    page = ctx.new_page()
    pages.append(page)
    watch(page)
    page.goto(BASE)
    expect(page.locator(".day")).to_have_count(7)

    # The service worker must control the page, or a reload would fail offline.
    page.evaluate("navigator.serviceWorker.ready.then(() => true)")
    page.reload()
    expect(page.locator(".day")).to_have_count(7)
    assert page.evaluate("Boolean(navigator.serviceWorker.controller)"), "page not controlled by the service worker"

    # Online: a recipe, planned on the last day (the smoke test left the first ones busy).
    add_recipe(page, "Offline soup", ["lentilles", "carottes"])
    plan_slot(page, 12, "offline soup")
    expect(page.get_by_role("button", name="Validate 1 new")).to_be_visible()
    expect(page.locator("#status-sync")).to_be_hidden(timeout=10000)  # sent

    # Cut the network: validate the plan, tick an ingredient, add an item by hand.
    offline = True
    ctx.set_offline(True)
    page.get_by_role("button", name="Validate 1 new").click()
    go(page, "3")
    expect(page.locator(".item", has_text="lentilles")).to_have_count(1)
    page.locator(".item", has_text="lentilles").click()
    expect(page.locator(".item.checked", has_text="lentilles")).to_have_count(1)
    page.fill(".add-item input", "Batteries")
    page.keyboard.press("Enter")
    expect(page.locator(".item", has_text="Batteries")).to_have_count(1)
    expect(page.locator("#status-sync")).to_contain_text("offline")
    expect(page.locator("#status-sync")).to_contain_text("3 pending")
    page.screenshot(path=f"{OUT}/offline-1.png")

    # The app starts without network, with the local data and the queue.
    page.reload()
    expect(page.locator(".view-title")).to_contain_text("Shopping")
    expect(page.locator(".item.checked", has_text="lentilles")).to_have_count(1)
    expect(page.locator(".item", has_text="Batteries")).to_have_count(1)
    expect(page.locator("#status-sync")).to_contain_text("3 pending")

    # Back online: the queue is sent.
    ctx.set_offline(False)
    offline = False
    page.evaluate("window.dispatchEvent(new Event('online'))")
    expect(page.locator("#status-sync")).to_be_hidden(timeout=10000)
    shopping = json.loads(ctx.request.get(f"{BASE}/api/shopping").text())
    items = {i["name"]: i for g in shopping["groups"] for i in g["items"]}
    assert "Batteries" in items, items.keys()
    assert items["lentilles"]["checked"], items["lentilles"]
    assert items["carottes"]["checked"] is False, items["carottes"]
    page.screenshot(path=f"{OUT}/offline-2.png")
    ctx.close()


with sync_playwright() as p:
    browser = p.chromium.launch()
    failed = False
    try:
        scenario(browser)
        print("offline: ok")
    except Exception as exc:
        failed = True
        print(f"offline: FAILED: {exc}")
        for i, pg in enumerate(pages):
            pg.screenshot(path=f"{OUT}/offline-failed-{i}.png")
    browser.close()
    for e in errors:
        print(e)
    sys.exit(1 if failed or errors else 0)
