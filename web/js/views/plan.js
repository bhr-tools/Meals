// Plan: the next seven days, two meals each.

import { h, icon } from '../dom.js';
import { api } from '../api.js';
import { openModal, openMenu, toast } from '../ui/overlay.js';
import { dayName, shortDate, sinceLabel } from '../dates.js';

const MEALS = [['lunch', 'Lunch'], ['dinner', 'Dinner']];

export function planView(app) {
  const slotAt = (key) => {
    const [date, meal] = key.split('/');
    return { date, meal, slot: app.state.plan?.days.find((d) => d.date === date)?.[meal] };
  };

  async function validate() {
    const { plan, today } = app.state;
    if (!plan?.pending) return toast('Nothing new to validate.');
    const res = await app.run(() => api.plan.validate(today));
    if (!res) return;
    await app.refresh();
    toast(`${res.validated} ${res.validated === 1 ? 'recipe' : 'recipes'} added to the shopping list`,
      { type: 'success', action: { label: 'View', run: () => app.go('shopping') } });
  }

  async function clearSlot(key) {
    const { date, meal, slot } = slotAt(key);
    if (!slot) return;
    if (await app.run(() => api.plan.clear(date, meal))) await app.refresh();
  }

  function copyDay(date, anchor) {
    const days = app.state.plan.days.filter((d) => d.date !== date);
    openMenu(anchor, days.map((d) => ({
      label: `${dayName(d.date, app.state.today)}`,
      hint: shortDate(d.date),
      run: async () => {
        if (await app.run(() => api.plan.copyDay(date, d.date))) {
          await app.refresh();
          toast(`Copied to ${dayName(d.date, app.state.today)}`);
        }
      },
    })), { title: `Copy ${dayName(date, app.state.today)} to…` });
  }

  /** Search dialog: pick a recipe, or turn what was typed into a note. */
  function choose(key) {
    const { date, meal } = slotAt(key);
    const words = (s) => s.toLowerCase().normalize('NFD').replace(/[̀-ͯ]/g, '').split(/\s+/).filter(Boolean);
    let items = [];
    let active = 0;

    const input = h('input', {
      type: 'text', class: 'picker-input', autocomplete: 'off', maxLength: 200,
      placeholder: 'Search a recipe or type a note',
    });
    const list = h('div', { class: 'picker-list' });

    const compute = () => {
      const q = words(input.value);
      const matches = app.state.recipes.filter((r) => {
        const title = words(r.title).join(' ');
        return q.every((w) => title.includes(w));
      });
      if (q.length) matches.sort((a, b) => Number(!words(a.title).join(' ').startsWith(q.join(' '))) - Number(!words(b.title).join(' ').startsWith(q.join(' '))));
      items = matches.map((recipe) => ({ recipe }));
      const text = input.value.trim();
      if (text) items.push({ text });
      active = Math.min(active, Math.max(0, items.length - 1));
    };

    const draw = () => {
      list.replaceChildren();
      if (!items.length) list.append(h('div', { class: 'picker-empty' }, 'No recipes yet. Type a note, or add recipes in the Recipes tab.'));
      items.forEach((item, i) => {
        const row = h('button', { type: 'button', class: ['picker-item', i === active && 'active'], onclick: () => pick(item) },
          h('span', { class: 'picker-label' }, item.recipe ? item.recipe.title : `Note: “${item.text}”`),
          item.recipe ? h('span', { class: 'picker-badge' }, sinceLabel(item.recipe.last_planned, app.state.today)) : null);
        list.append(row);
        if (i === active) requestAnimationFrame(() => row.scrollIntoView({ block: 'nearest' }));
      });
    };

    async function pick(item) {
      modal.close();
      const ok = await app.run(() => item.recipe
        ? api.plan.setRecipe(date, meal, item.recipe.id)
        : api.plan.setText(date, meal, item.text));
      if (ok) await app.refresh();
    }

    input.addEventListener('input', () => { active = 0; compute(); draw(); });
    input.addEventListener('keydown', (e) => {
      const down = e.key === 'ArrowDown' || (e.ctrlKey && e.key === 'n');
      const up = e.key === 'ArrowUp' || (e.ctrlKey && e.key === 'p');
      if (down || up) {
        e.preventDefault();
        if (items.length) active = (active + (down ? 1 : -1) + items.length) % items.length;
        draw();
      } else if (e.key === 'Enter') {
        e.preventDefault();
        if (items[active]) pick(items[active]);
      }
    });

    const modal = openModal({
      className: 'picker',
      body: [h('div', { class: 'picker-search' }, icon('search'), input), list],
    });
    compute();
    draw();
  }

  return {
    title: 'Plan',
    hints: [[['j', 'k'], 'move'], [['Enter'], 'choose'], [['x'], 'clear'], [['c'], 'copy day'], [['v'], 'validate']],

    actions() {
      const pending = app.state.plan?.pending || 0;
      return pending
        ? [h('button', { type: 'button', class: 'button primary', onclick: validate }, `Validate ${pending} new`)]
        : [];
    },

    render() {
      const { plan, today } = app.state;
      const rows = [];
      const el = h('div', { class: 'days' });
      for (const day of plan?.days || []) {
        const head = h('div', { class: 'day-head' },
          h('span', { class: 'day-name' }, dayName(day.date, today)),
          h('span', { class: 'day-date' }, shortDate(day.date)));
        const copy = h('button', { type: 'button', class: 'icon-button', title: 'Copy this day to…', onclick: (e) => copyDay(day.date, e.currentTarget) }, icon('copy'));
        head.append(copy);
        const box = h('div', { class: ['day', day.date === today && 'today'] }, head);
        for (const [meal, label] of MEALS) {
          const slot = day[meal];
          const key = `${day.date}/${meal}`;
          const text = slot ? (slot.recipe_id ? slot.title : slot.text) : 'Nothing planned';
          const row = h('div', {
            class: ['row', 'slot', !slot && 'empty', slot && !slot.recipe_id && 'note'],
            dataset: { key },
            onclick: () => { app.select(key); choose(key); },
          },
          h('span', { class: 'slot-meal' }, label),
          h('span', { class: 'slot-title' }, text),
          slot?.recipe_id ? h('span', { class: ['slot-state', slot.status], title: { pending: 'Not in the shopping list yet', listed: 'In the shopping list', done: 'Shopped' }[slot.status] }) : null);
          rows.push({ key, el: row });
          box.append(row);
        }
        el.append(box);
      }
      return { el, rows };
    },

    key(k, row) {
      if (k === 'v') { validate(); return true; }
      if (!row) return false;
      switch (k) {
        case 'Enter': case 'e': case 'a': choose(row.key); return true;
        case 'x': case 'd': clearSlot(row.key); return true;
        case 'c': copyDay(slotAt(row.key).date, row.el); return true;
      }
      return false;
    },
  };
}
