// Port of Normalize and Classify (internal/aisle/aisle.go). The Go code is the
// reference: tests/fixtures/normalize.json is checked against both. Keep them
// identical.

const ACCENTS = {
  'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e', 'à': 'a', 'â': 'a', 'ä': 'a', 'î': 'i', 'ï': 'i',
  'ô': 'o', 'ö': 'o', 'ù': 'u', 'û': 'u', 'ü': 'u', 'ç': 'c', 'ñ': 'n', 'œ': 'oe', 'æ': 'ae',
};
const utf8Length = (s) => new TextEncoder().encode(s).length;

/**
 * Reduces a name to a comparable form: lower case, no accents or punctuation,
 * single spaces and a naive singular, so that "Courgettes" and "courgette"
 * are the same ingredient.
 */
export function normalize(s) {
  const lowered = s.toLowerCase().replace(/[éèêëàâäîïôöùûüçñœæ]/g, (c) => ACCENTS[c]);
  // Go keeps letters and decimal digits (unicode.IsLetter, IsDigit): \p{Nd}, not \p{N}.
  const spaced = lowered.replace(/[^\p{L}\p{Nd}]/gu, ' ');
  return spaced.split(/\s+/).filter(Boolean).map(singular).join(' ');
}

// Go counts bytes in len(w): compare UTF-8 lengths, not JS lengths.
function singular(w) {
  if (utf8Length(w) > 3 && w.endsWith('s') && !w.endsWith('ss') && !w.endsWith('us') && !w.endsWith('is')) {
    return w.slice(0, -1);
  }
  return w;
}

/**
 * The key of the aisle (such as "produce") that holds a normalized name, or
 * null. dictionary is the snapshot's: [{ words, key }], most specific first.
 * Keywords match whole words.
 */
export function classify(norm, dictionary) {
  const words = norm.split(/\s+/).filter(Boolean);
  for (const e of dictionary) {
    if (containsRun(words, e.words)) return e.key;
  }
  return null;
}

function containsRun(words, run) {
  for (let i = 0; i + run.length <= words.length; i++) {
    if (run.every((w, j) => words[i + j] === w)) return true;
  }
  return false;
}
