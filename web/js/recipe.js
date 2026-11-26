// A recipe, shown to be read (from the plan or the recipe list) or edited.

import { h, autoGrow, isTextInput } from './dom.js';
import { api } from './api.js';
import { openModal, topModal, isPopoverOpen, confirmDialog, toast } from '/kit/js/overlay.js';
import { sinceLabel } from './dates.js';

const URL_RE = /https?:\/\/[^\s<>"]+[^\s<>".,;:!?)\]]/g;

/** Text with its links made clickable (as text nodes and anchors, never HTML). */
function linkify(text) {
  const out = [];
  let last = 0;
  for (const m of text.matchAll(URL_RE)) {
    if (m.index > last) out.push(text.slice(last, m.index));
    out.push(h('a', { href: m[0], target: '_blank', rel: 'noopener noreferrer' }, m[0]));
    last = m.index + m[0].length;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

/**
 * Lays out free notes: blank lines separate paragraphs, "1." lines become a
 * numbered list and "-" lines a bulleted one.
 */
export function renderNotes(notes) {
  const out = [];
  let para = [];
  let list = null; // { el, kind }
  const flushPara = () => {
    if (para.length) out.push(h('p', {}, para.flatMap((line, i) => (i ? [h('br'), ...linkify(line)] : linkify(line)))));
    para = [];
  };
  for (const raw of notes.replace(/\r/g, '').split('\n')) {
    const line = raw.trim();
    const num = line.match(/^(\d+)[.)]\s+(.*)$/);
    const bullet = line.match(/^[-*•]\s+(.*)$/);
    const kind = num ? 'ol' : bullet ? 'ul' : null;
    if (kind) {
      flushPara();
      if (list?.kind !== kind) {
        list = { kind, el: h(kind, kind === 'ol' && num[1] !== '1' ? { start: Number(num[1]) } : {}) };
        out.push(list.el);
      }
      list.el.append(h('li', {}, linkify(num ? num[2] : bullet[1])));
    } else if (!line) {
      flushPara();
      list = null;
    } else if (list && /^\s/.test(raw)) {
      // An indented line continues the previous list item.
      list.el.lastChild.append(h('br'), ...linkify(line));
    } else {
      list = null;
      para.push(line);
    }
  }
  flushPara();
  return out;
}

function plannedLabel(iso, today) {
  const since = sinceLabel(iso, today);
  if (since === 'never') return 'Never planned';
  return since === 'today' || since.startsWith('in ') ? `Planned ${since}` : `Last planned ${since}`;
}

/**
 * Shows a recipe to be read. `actions` adds buttons before Edit:
 * [{ label, run(close) }].
 */
export function showRecipe(app, recipe, { actions = [] } = {}) {
  let current = recipe;
  const body = h('div', { class: 'recipe-sheet' });
  let titleEl = null;

  const draw = () => {
    titleEl.textContent = current.title;
    const ingredients = current.ingredients.length
      ? h('ul', { class: 'recipe-ingredients' }, current.ingredients.map((i) => h('li', {}, i)))
      : h('p', { class: 'recipe-empty' }, 'No ingredients.');
    const notes = current.notes?.trim()
      ? h('section', {}, h('h3', { class: 'recipe-heading' }, 'Notes'), h('div', { class: 'recipe-notes' }, renderNotes(current.notes)))
      : null;
    body.replaceChildren(
      h('section', {}, h('h3', { class: 'recipe-heading' }, 'Ingredients'), ingredients),
      ...(notes ? [notes] : []),
      h('p', { class: 'recipe-meta' }, plannedLabel(current.last_planned, app.state.today)));
  };

  const edit = () => editRecipe(app, current, {
    onSaved: (saved) => { current = app.state.recipes.find((r) => r.id === saved.id) || { ...current, ...saved }; draw(); },
    onDeleted: () => modal.close(),
  });

  // e edits, while this sheet is the top layer (also after the editor closed).
  const onKey = (e) => {
    if (e.key !== 'e' || e.ctrlKey || e.metaKey || e.altKey || e.defaultPrevented) return;
    if (topModal() !== modal || isPopoverOpen() || isTextInput(e.target)) return;
    e.preventDefault();
    edit();
  };
  window.addEventListener('keydown', onKey);

  const modal = openModal({
    className: 'recipe-view',
    onClose: () => window.removeEventListener('keydown', onKey),
    autofocus: false,
    title: current.title,
    body,
    footer: [
      ...actions.map((a) => h('button', { type: 'button', class: 'button', onclick: () => a.run(() => modal.close()) }, a.label)),
      h('button', { type: 'button', class: 'button primary', onclick: edit }, 'Edit'),
    ],
  });
  titleEl = modal.el.querySelector('.modal-title');
  draw();
  return modal;
}

/** Creates (recipe = null) or edits a recipe. */
export function editRecipe(app, recipe, { onSaved, onDeleted } = {}) {
  const title = h('input', { type: 'text', class: 'input', maxLength: 200, placeholder: 'Recipe name', value: recipe?.title || '' });
  const ingredients = h('textarea', {
    class: 'input field-area', rows: 8, spellcheck: false,
    placeholder: 'One ingredient per line\nzucchini\ncream\nonion',
    value: recipe ? recipe.ingredients.join('\n') : '',
  });
  const notes = h('textarea', {
    class: 'input field-area', rows: 5, spellcheck: false,
    placeholder: 'Steps, a link, a cooking tip… never added to the shopping list.\n\n1. A numbered line becomes a step\n- a dash makes a bullet',
    value: recipe?.notes || '',
  });
  const grow = (t) => t.addEventListener('input', () => autoGrow(t, 150));

  let modal;
  async function save() {
    if (!title.value.trim()) { title.focus(); return; }
    const payload = { title: title.value, ingredients: ingredients.value.split('\n'), notes: notes.value };
    const saved = await app.run(() => (recipe ? api.recipes.update(recipe.id, payload) : api.recipes.create(payload)));
    if (!saved) return;
    modal.close();
    await app.refresh();
    app.select(saved.id);
    onSaved?.(saved);
    toast(recipe ? 'Recipe saved' : 'Recipe added', { type: 'success' });
  }
  async function remove() {
    const ok = await confirmDialog('Delete recipe', `“${recipe.title}” will also be removed from the plan.`, { okText: 'Delete' });
    if (!ok) return;
    if (await app.run(() => api.recipes.remove(recipe.id))) {
      modal.close();
      onDeleted?.();
      await app.refresh();
    }
  }

  const field = (label, control) => h('label', { class: 'field' }, h('span', { class: 'field-label' }, label), control);
  const submitOnCtrlEnter = (e) => {
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) { e.preventDefault(); save(); }
  };
  title.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') { e.preventDefault(); ingredients.focus(); }
  });
  for (const t of [ingredients, notes]) t.addEventListener('keydown', submitOnCtrlEnter);
  grow(ingredients);
  grow(notes);

  modal = openModal({
    title: recipe ? 'Edit recipe' : 'New recipe',
    className: 'recipe',
    body: [
      field('Name', title),
      field('Ingredients', ingredients),
      h('details', { class: 'notes', open: !!recipe?.notes }, h('summary', {}, 'Notes'), notes),
    ],
    footer: [
      recipe ? h('button', { type: 'button', class: 'button danger push-left', onclick: remove }, 'Delete') : null,
      h('button', { type: 'button', class: 'button', onclick: () => modal.close() }, 'Cancel'),
      h('button', { type: 'button', class: 'button primary', onclick: save }, 'Save'),
    ],
  });
  requestAnimationFrame(() => { autoGrow(ingredients, 150); autoGrow(notes, 150); });
  return modal;
}
