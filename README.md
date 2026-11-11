# Meals

A minimalist meal planner and shopping list. Plan the next seven days (lunch
and dinner), keep recipes as a title plus a list of ingredients, and get a
shopping list grouped by aisle for the recipes you just added.

- **Plan**: today and the six days after it. A slot holds a recipe or a free
  note ("leftovers", "out"). New recipes are *pending* until you validate them.
- **Recipes**: a title, ingredient names (no quantities) and free notes. Notes
  never reach the shopping list.
- **Shopping**: the ingredients of the validated recipes, by aisle, each with
  the recipes that need it and how many planned meals (`Bolognese ×2`). Tick
  what you already have. *Done* closes the list; the next validation starts a
  new one with only the new recipes.

Aisles come from a built-in keyword dictionary (French and English,
`internal/aisle/dictionary.txt`). They can be renamed, reordered, added and
removed; correcting one ingredient's aisle is remembered.

**Works offline**: changes are kept on the device and sent when the
connection returns. The app starts without network (a service worker keeps its
files) and shows all your data from a copy kept in the browser (IndexedDB);
every change is applied on the device at once and queued, even if the app is
closed before the network is back. The status bar shows `offline` and the
number of changes waiting. *Lock* erases the data kept on the device.

Technically, `GET /api/sync` returns the whole data (recipes, plan, shopping
state, aisles and the aisle dictionary) with an ETag, and `POST /api/sync`
applies a batch of changes in order, then returns the new data. Each change
has a client-chosen UUID, remembered for 30 days in the `sync_ops` table, so a
batch sent twice is applied once. The server stays the authority: the last
change to reach it wins, and every snapshot it sends replaces the device's
guess. The shopping list is computed on the device from the same data
(`web/js/aisle.js` is a port of `internal/aisle`; `tests/fixtures/normalize.json`
checks both). Password changes need the network.

The interface is in English, Rosé Pine (dark/light, following the system),
with Vim keys (`?` lists them) and a bottom tab bar on phones.

## Run

```sh
go run ./cmd/server -port 8080 -data-dir ./data
```

Flags default to environment variables: `HOST`, `PORT`, `DATA_DIR`, `DB_PATH`,
`NO_AUTH`. The first visit asks for a master password.

## NixOS

```nix
inputs.meals.url = "github:bhr-tools/Meals";
# modules = [ meals.nixosModules.default ];
services.meals = {
  enable = true;
  instances.ben.port = 9920;
};
```

Data lives in `/var/lib/meals-<instance>/meals.db`.

## Tests

```sh
go test ./...
node --test tests/js/*.test.mjs   # offline state, reducer and sync engine
tests/e2e/run.sh   # Playwright from nixpkgs, screenshots in tests/e2e/out
```
