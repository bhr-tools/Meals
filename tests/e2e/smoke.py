"""End-to-end test: plan meals, validate, shop, finish, plan a second batch."""
import os
from playwright.sync_api import sync_playwright, expect

BASE = os.environ["BASE"]
OUT = os.environ.get("OUT", ".")


def go(page, key):
    """Switch tab with the keyboard, leaving any focused field first."""
    page.keyboard.press("Escape")
    page.keyboard.press(key)


def add_recipe(page, title, ingredients, notes=""):
    go(page, "2")
    page.get_by_role("button", name="New recipe").click()
    page.fill(".modal.recipe input", title)
    page.fill(".modal.recipe textarea >> nth=0", "\n".join(ingredients))
    if notes:
        page.click(".modal.recipe summary")
        page.fill(".modal.recipe textarea >> nth=1", notes)
    page.get_by_role("button", name="Save").click()
    expect(page.locator(".modal.recipe")).to_have_count(0)


def plan_slot(page, index, text):
    """Choose in the index-th slot (0 = today's lunch)."""
    go(page, "1")
    page.locator(".slot").nth(index).click()
    page.fill(".picker-input", text)
    page.keyboard.press("Enter")
    expect(page.locator(".modal-overlay")).to_have_count(0)


def run(p):
    browser = p.chromium.launch()
    page = browser.new_page(viewport={"width": 1100, "height": 760})
    errors = []
    page.on("pageerror", lambda e: errors.append(str(e)))
    page.on("console", lambda m: m.type == "error" and errors.append(m.text))
    page.goto(BASE)
    expect(page.locator(".view-title")).to_have_text("Plan")
    expect(page.locator(".day")).to_have_count(7)
    expect(page.locator(".slot")).to_have_count(14)
    expect(page.locator(".day.today .day-name")).to_have_text("Today")

    # Recipes: ingredients only, notes kept apart.
    add_recipe(page, "Pasta bolognese", ["pâtes", "oignon", "boeuf haché", "sauce tomate"], "Simmer 30 min")
    add_recipe(page, "Chili con carne", ["riz", "oignons", "boeuf haché", "haricots rouges"])
    expect(page.locator(".row")).to_have_count(2)
    page.fill(".search-box input", "chili")
    expect(page.locator(".row")).to_have_count(1)
    page.fill(".search-box input", "")
    page.screenshot(path=f"{OUT}/recipes.png")

    # Plan: two bolognese and two chili, one note. Nothing is listed yet.
    plan_slot(page, 1, "bolo")
    plan_slot(page, 3, "bolo")
    plan_slot(page, 5, "chili")
    plan_slot(page, 7, "chili")
    plan_slot(page, 0, "out tonight")  # free text: type it, go down to the note entry
    expect(page.locator(".slot-skip")).to_have_count(4)
    expect(page.get_by_role("button", name="Validate 4 new")).to_be_visible()
    # "No list" keeps one recipe out of the list; Undo brings it back.
    page.locator(".slot-skip").first.click()
    expect(page.locator(".slot-skip")).to_have_count(3)
    expect(page.get_by_role("button", name="Validate 3 new")).to_be_visible()
    page.get_by_role("button", name="Undo").click()
    expect(page.get_by_role("button", name="Validate 4 new")).to_be_visible()
    page.screenshot(path=f"{OUT}/plan.png")
    go(page, "3")
    expect(page.locator(".empty-list")).to_be_visible()

    # Validate: the list shows each ingredient with its recipes.
    go(page, "1")
    page.get_by_role("button", name="Validate 4 new").click()
    expect(page.locator(".slot-skip")).to_have_count(0)
    go(page, "3")
    onion = page.locator(".item", has_text="oignon")
    expect(onion.locator(".ref")).to_have_count(2)
    expect(onion).to_contain_text("Pasta bolognese ×2")
    expect(onion).to_contain_text("Chili con carne ×2")
    expect(page.locator(".item", has_text="Simmer")).to_have_count(0)  # notes stay out
    group_titles = page.locator(".group-title").all_inner_texts()
    assert group_titles[0] == "Produce" and group_titles[-1] in ("Pantry", "Spices & sauces", "Other"), group_titles
    page.screenshot(path=f"{OUT}/shopping.png")

    # Tick, add by hand, change an aisle.
    page.locator(".item", has_text="riz").click()
    expect(page.locator(".item.checked")).to_have_count(1)
    page.fill(".add-item input", "papier toilette")
    page.keyboard.press("Enter")
    expect(page.locator(".item", has_text="papier toilette")).to_have_count(1)
    page.locator(".item", has_text="haricots rouges").locator(".row-more").click(force=True)
    page.get_by_role("menuitem", name="Change aisle").click()
    page.get_by_role("menuitem", name="Frozen").click()
    expect(page.locator(".group", has_text="Frozen")).to_contain_text("haricots rouges")

    # Keyboard: j moves the cursor, Space ticks.
    page.keyboard.press("g")
    page.keyboard.press("g")
    page.keyboard.press(" ")
    expect(page.locator(".item.checked")).to_have_count(2)

    # Aisles dialog: add one and remove it.
    page.get_by_role("button", name="Aisles").click()
    page.locator(".modal.aisles").get_by_role("button", name="Add aisle").click()
    page.fill(".modal.dialog input", "Bio")
    page.keyboard.press("Enter")
    expect(page.locator(".aisle-row", has_text="Bio")).to_have_count(1)
    page.keyboard.press("Escape")

    # Finish: the list empties, the plan keeps its meals.
    page.get_by_role("button", name="Done").click()
    page.get_by_role("button", name="Finish").click()
    expect(page.locator(".empty-list")).to_be_visible()
    go(page, "1")
    expect(page.locator(".slot-skip")).to_have_count(0)

    # Second batch: only the new recipe reaches the list.
    plan_slot(page, 9, "bolo")
    page.get_by_role("button", name="Validate 1 new").click()
    go(page, "3")
    expect(page.locator(".item", has_text="oignon").locator(".ref")).to_have_count(1)
    expect(page.locator(".item", has_text="riz")).to_have_count(0)

    # Phone layout: bottom tabs, full-screen dialog.
    page.set_viewport_size({"width": 390, "height": 780})
    expect(page.locator(".tabbar")).to_be_visible()
    expect(page.locator(".sidebar")).to_be_hidden()
    page.screenshot(path=f"{OUT}/phone-shopping.png")
    page.locator(".tab", has_text="Plan").click()
    page.locator(".slot").nth(2).click()
    expect(page.locator(".modal.picker")).to_be_visible()
    box = page.locator(".modal.picker").bounding_box()
    assert box["width"] >= 389 and box["x"] == 0, box
    page.wait_for_timeout(400)
    page.screenshot(path=f"{OUT}/phone-picker.png")
    page.keyboard.press("Escape")
    page.screenshot(path=f"{OUT}/phone-plan.png")
    page.locator(".tab", has_text="Recipes").click()
    expect(page.locator(".tab.active")).to_have_text("Recipes")
    page.wait_for_timeout(300)
    page.screenshot(path=f"{OUT}/phone-recipes.png")
    page.locator(".list .row").first.click()
    expect(page.locator(".modal.recipe")).to_be_visible()
    page.wait_for_timeout(400)
    page.screenshot(path=f"{OUT}/phone-recipe.png")
    page.keyboard.press("Escape")
    page.emulate_media(color_scheme="dark")
    page.locator(".tab", has_text="Plan").click()
    page.wait_for_timeout(300)
    page.screenshot(path=f"{OUT}/phone-plan-dark.png")
    page.emulate_media(color_scheme="light")

    assert not errors, errors
    browser.close()
    print("e2e ok")


with sync_playwright() as p:
    run(p)
