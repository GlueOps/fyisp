// Details → Storage, a campaign's results: each round over time
// (api/storage?part=campaign's rounds, cells and throughput, and
// part=timings). One metric at a time (download or upload speed, or the
// time to first byte of fresh connections), a line per target. A
// recurring campaign's hours between its windows are left out: the x axis
// is the window's number plus how far into it a round was, with a break
// mark between windows. Plain ES module, no build step.
// @ts-check

import { h, fetchJSON, css, showTip, hideTip, fmtMbps, fmtMs, clock, dialog, median } from './core.js';
import { cross, niceCeil, swatch } from './speedtime.js';
import { size } from './storageform.js';

/** @typedef {{at:number[],n:number[],dns_ms:(number|null)[],connect_ms:(number|null)[],tls_ms:(number|null)[],ttfb_ms:(number|null)[]}} Timings */

const uPlot = /** @type {any} */ (window).uPlot;
const FONT = '11px system-ui, -apple-system, "Segoe UI", sans-serif';
const GAP = 0.06; // the room a break between windows takes, in windows
// A second cue beside the colour, by target.
const DASH = [undefined, [8, 4], [16, 5], [10, 4, 3, 4]];
const METRICS = /** @type {const} */ ({
  ttfb: { label: 'Time to first byte', unit: 'ms', fmt: (/** @type {number} */ v) => `${fmtMs(v)} ms` },
  down: { label: 'Download speed', unit: 'Mbit/s', fmt: (/** @type {number} */ v) => fmtMbps(v * 1e6) },
  up: { label: 'Upload speed', unit: 'Mbit/s', fmt: (/** @type {number} */ v) => fmtMbps(v * 1e6) },
});
/** @typedef {keyof typeof METRICS} Metric */

/** "Tue 17". */
function dayShort(/** @type {number} */ ms) { const d = new Date(ms); return `${d.toLocaleDateString([], { weekday: 'short' })} ${d.getDate()}`; }

/**
 * Each round's place on the x axis: rounds more than 3 intervals apart (a
 * schedule's night) start a new window; a round's x is its window's number
 * (times 1 + GAP) plus how far into the window it is. A stretch fyisp
 * wasn't running (a gap round) stays inside its window, so the line breaks
 * there instead.
 * @param {any[]} rounds @param {number} interval ms
 */
export function placeRounds(rounds, interval) {
  /** @type {{start:number,end:number,first:number}[]} the windows: their times and first round */
  const wins = [];
  rounds.forEach((r, i) => {
    const t = r.start || r.due;
    const w = wins[wins.length - 1];
    if (!w || t - w.end > 3 * interval) wins.push({ start: t, end: t, first: i });
    const end = r.outcome === 'gap' ? r.end || t : t;
    wins[wins.length - 1].end = Math.max(wins[wins.length - 1].end, end);
  });
  const x = new Array(rounds.length);
  wins.forEach((w, k) => {
    const span = w.end - w.start + interval;
    const last = k + 1 < wins.length ? wins[k + 1].first : rounds.length;
    for (let i = w.first; i < last; i++) x[i] = k * (1 + GAP) + ((rounds[i].start || rounds[i].due) - w.start + interval / 2) / span;
  });
  return { x, wins: wins.map((w, k) => ({ ...w, x0: k * (1 + GAP), x1: k * (1 + GAP) + 1 })) };
}

export class StorageTime {
  /** @param {{name:(id:string)=>string}} o */
  constructor(o) {
    this.o = o;
    /** @type {Metric} */
    this.metric = 'ttfb';
    /** @type {string[]} the targets shown (none: all) */
    this.only = [];
    /** @type {[number, number]|null} */
    this.zoom = null;
    /** @type {any} part=campaign */
    this.c = null;
    /** @type {Object<string, Timings>|null} */
    this.timings = null;
    this.timingsFor = '';
    /** @type {any} the plot */
    this.u = null;
    this.sig = '';
    this.seq = 0;

    this.metricEl = h('div', { class: 'seg sm', role: 'group', 'aria-label': 'What to show' });
    this.pickBtn = h('button', { class: 'btn sm', type: 'button', 'aria-haspopup': 'dialog', onclick: () => this.pick() });
    this.allBtn = h('button', { class: 'btn sm', type: 'button', text: 'Show all', hidden: true, onclick: () => this.setOnly([]) });
    this.zoomBtn = h('button', { class: 'btn sm spt-zoom', type: 'button', text: 'Zoomed ×', hidden: true, onclick: () => this.setZoom(null) });
    this.legendEl = h('div', { class: 'spt-legend' });
    this.plotEl = h('div', { class: 'spt-plot' });
    this.lanesEl = /** @type {HTMLCanvasElement} */ (h('canvas', { class: 'spt-lanes', hidden: true }));
    this.chartEl = h('div', { class: 'spt-chart', role: 'img' }, this.plotEl, this.lanesEl);
    this.hintEl = h('p', { class: 'spt-hint', hidden: true });
    this.emptyEl = h('p', { class: 'muted', hidden: true });
    this.cornerEl = h('p', { class: 'spt-corner muted small' });
    this.capEl = h('p', { class: 'spt-cap' });
    this.say = h('p', { class: 'sr-only', role: 'status' });
    this.el = h('section', { class: 'st-time', 'aria-labelledby': 'st-time-h' },
      h('div', { class: 'sp-sec-head' }, h('h3', { id: 'st-time-h', text: 'Each round over time' }),
        h('div', { class: 'sp-ctl' }, this.metricEl, this.pickBtn, this.allBtn, this.zoomBtn)),
      this.legendEl, this.emptyEl, this.chartEl, this.hintEl, this.cornerEl, this.capEl, this.say);
    new ResizeObserver(() => this.resize()).observe(this.el);
  }

  /** The campaign shown changed or got a round: draw it, reading its timings when they are new. @param {any} c */
  async set(c) {
    const first = !this.c || this.c.campaign.id !== c.campaign.id;
    this.c = c;
    if (first) {
      this.only = []; this.zoom = null; this.timings = null; this.sig = '';
      // A campaign without keys has no transfers: its timings, then.
      if (!this.keyed().length) this.metric = 'ttfb';
    }
    const key = `${c.campaign.id}:${c.rounds.length}:${c.campaign.rounds_done}`;
    if (key !== this.timingsFor) {
      const seq = ++this.seq;
      try {
        const t = await fetchJSON('api/storage?' + new URLSearchParams({ part: 'timings', id: String(c.campaign.id) }));
        if (seq !== this.seq) return;
        this.timings = t.timings;
        this.timingsFor = key;
      } catch {
        if (seq !== this.seq) return;
        // The speeds still draw; the timings show again when fyisp answers.
      }
    }
    this.render();
  }

  /** The campaign's targets that had keys (their transfers ran). */
  keyed() { return this.c ? this.c.campaign.targets.filter((/** @type {any} */ t) => t.mode === 'bucket') : []; }

  /** The targets the metric has values for. */
  candidates() { return this.c ? (this.metric === 'ttfb' ? this.c.campaign.targets : this.keyed()) : []; }

  shown() { const all = this.candidates(); return this.only.length ? all.filter((/** @type {any} */ t) => this.only.includes(t.id)) : all; }

  /** @param {string[]} ids */
  setOnly(ids) { this.only = ids; this.render(); this.say.textContent = ids.length === 1 ? `Showing ${this.o.name(ids[0])} alone.` : 'Showing every target.'; }

  /** @param {Metric} m */
  setMetric(m) { if (m === this.metric) return; this.metric = m; this.only = this.only.filter((id) => this.candidates().some((/** @type {any} */ t) => t.id === id)); this.render(); }

  /** @param {[number, number]|null} z */
  setZoom(z) {
    this.zoom = z;
    this.u?.setScale('x', { min: this.view()[0], max: this.view()[1] });
    this.zoomBtn.hidden = !z;
    this.say.textContent = z ? 'Zoomed in.' : 'Zoomed out.';
  }

  view() { return this.zoom || [0, this.place ? this.place.wins.length * (1 + GAP) - GAP : 1]; }

  themeChanged() { this.sig = ''; if (this.c) this.render(); }

  resize() {
    const w = this.width(), u = this.u;
    if (u && Math.abs(u.width - w) > 1) u.setSize({ width: w, height: u.height });
    this.drawLanes();
  }

  width() { return Math.max(200, this.chartEl.clientWidth || this.el.clientWidth - 2); }

  /** Each shown target's values by round, for the metric (null: none). @param {any} t */
  values(t) {
    const c = this.c, idx = new Map(c.rounds.map((/** @type {any} */ r, /** @type {number} */ i) => [r.start || r.due, i]));
    const out = new Array(c.rounds.length).fill(null);
    if (this.metric === 'ttfb') {
      const s = this.timings?.[t.id];
      if (s) s.at.forEach((at, k) => { const i = idx.get(at); if (i != null) out[i] = s.ttfb_ms[k]; });
    } else {
      const s = c.throughput?.[t.id];
      if (s) s.at.forEach((/** @type {number} */ at, /** @type {number} */ k) => { const i = idx.get(at); const v = (this.metric === 'down' ? s.down_bps : s.up_bps)[k]; if (i != null && v != null) out[i] = v / 1e6; });
    }
    return out;
  }

  /** The rounds where the target failed (the grid's 'f'). @param {any} t */
  failed(t) { const cells = this.c.cells?.[t.id] || ''; return this.c.rounds.map((/** @type {any} */ _, /** @type {number} */ i) => cells[i] === 'f'); }

  /** @param {any} t */
  slot(t) { return this.c.campaign.targets.findIndex((/** @type {any} */ x) => x.id === t.id) + 1; }

  /** A target's colour: the palette's 8 by its place in the campaign. @param {any} t */
  colour(t) { return css('--s' + (((this.slot(t) - 1) % 8) + 1)); }

  render() {
    const c = this.c;
    if (!c) return;
    const keyed = this.keyed().length > 0;
    this.metricEl.replaceChildren(.../** @type {Metric[]} */ (['ttfb', 'down', 'up']).map((m) => h('button', { type: 'button', text: METRICS[m].label, 'aria-pressed': String(m === this.metric),
      disabled: m !== 'ttfb' && !keyed, title: m !== 'ttfb' && !keyed ? 'No target here had keys, so no transfers ran.' : null, onclick: () => this.setMetric(m) })));
    const all = this.candidates(), shown = this.shown();
    this.pickBtn.textContent = `Targets: ${shown.length === all.length ? 'all' : shown.length === 1 ? this.o.name(shown[0].id) : `${shown.length} of ${all.length}`} ▾`;
    this.allBtn.hidden = shown.length === all.length;
    this.place = placeRounds(c.rounds, c.campaign.interval_s * 1000);
    const series = shown.map((/** @type {any} */ t) => ({ t, v: this.values(t), bad: this.failed(t) }));
    const any = series.some((s) => s.v.some((/** @type {any} */ x) => x != null) || s.bad.some(Boolean));
    this.emptyEl.hidden = any;
    this.emptyEl.textContent = this.metric === 'ttfb' && !this.timings ? 'Loading the timings…' : 'No results to draw yet.';
    this.chartEl.hidden = !any;
    if (!any) { this.legendEl.replaceChildren(); this.hintEl.hidden = true; this.cornerEl.textContent = this.capEl.textContent = ''; return; }
    const m = METRICS[this.metric];
    const top = Math.max(0, ...series.flatMap((s) => s.v.filter((/** @type {any} */ x) => x != null)));
    this.yTop = niceCeil(top * 1.05);
    // A target whose line lies flat at this scale: say so, and offer it alone.
    const small = series.length > 1 ? series.filter((s) => { const mx = Math.max(0, ...s.v.filter((/** @type {any} */ x) => x != null)); return mx > 0 && mx < 0.1 * this.yTop; }) : [];
    this.hintEl.hidden = !small.length;
    if (small.length) {
      const names = small.map((s) => this.o.name(s.t.id));
      this.hintEl.replaceChildren(`${names.join(' and ')} ${names.length > 1 ? 'are' : 'is'} too small to read at this scale. `,
        h('button', { class: 'btn sm', type: 'button', text: names.length > 1 ? 'Show only these' : 'Show it alone', onclick: () => this.setOnly(small.map((s) => s.t.id)) }));
    }
    this.renderLegend(series, m);
    const step = c.campaign.interval_s / 60;
    this.cornerEl.textContent = `one point per round${c.rounds.length > 1 ? ` · ${step === 1 ? 'a minute' : `${step} minutes`} apart` : ''}${this.place.wins.length > 1 ? ' · the hours between windows are left out' : ''}`;
    const unkeyed = this.metric === 'ttfb' && shown.some((/** @type {any} */ t) => t.mode !== 'bucket') && shown.some((/** @type {any} */ t) => t.mode === 'bucket');
    this.capEl.textContent = (this.metric === 'ttfb'
      ? 'How long a fresh connection took to get its first answer (the median of the round’s): name lookup, connecting, encryption and the server’s reply.'
      : `How fast each round’s ${this.metric === 'down' ? 'download' : 'upload'} of ${size(this.metric === 'down' ? c.campaign.down_bytes : c.campaign.up_bytes)} ran. A transfer can be limited by the endpoint, not your line.`)
      + (unkeyed ? ' Targets without keys answer a simpler request, so their times are not like for like.' : '');
    this.chartEl.setAttribute('aria-label', series.map((s) => {
      const vals = s.v.filter((/** @type {any} */ x) => x != null), bad = s.bad.filter(Boolean).length;
      return `${this.o.name(s.t.id)}: ${vals.length ? `usually ${m.fmt(/** @type {number} */ (median(vals)))}, at worst ${m.fmt(this.metric === 'ttfb' ? Math.max(...vals) : Math.min(...vals))}` : 'no results'}${bad ? `, failed in ${bad} ${bad === 1 ? 'round' : 'rounds'}` : ''}.`;
    }).join(' '));
    this.lanes = series.length > 1 && series.some((s) => s.bad.some(Boolean));
    this.draw(series, m);
  }

  /** @param {{t:any,v:(number|null)[],bad:boolean[]}[]} series @param {any} m */
  renderLegend(series, m) {
    const several = series.length > 1;
    this.legendEl.replaceChildren(...series.map(({ t, v }) => {
      const vals = v.filter((x) => x != null), slot = this.slot(t);
      const sw = swatch(this.colour(t), several ? DASH[(slot - 1) % DASH.length] : undefined);
      const kids = [sw, this.o.name(t.id), h('span', { class: 'muted', text: `· ${vals.length ? m.fmt(/** @type {number} */ (median(vals))) : 'no results'}` }),
        // Only beside targets that had keys: then the requests differ.
        t.mode !== 'bucket' && this.metric === 'ttfb' && series.some((x) => x.t.mode === 'bucket') ? h('span', { class: 'spt-tag', text: 'no keys: simpler request' }) : null];
      return several ? h('button', { type: 'button', class: 'spt-key', title: 'Show this one alone', onclick: () => { this.setOnly([t.id]); this.pickBtn.focus(); } }, ...kids)
        : h('span', { class: 'spt-key' }, ...kids);
    }), series.some((x) => x.bad.some(Boolean)) ? h('span', { class: 'spt-key muted' }, h('span', { 'aria-hidden': 'true', text: '×' }), 'failed') : '');
  }

  /** @param {{t:any,v:(number|null)[],bad:boolean[]}[]} series @param {any} m */
  draw(series, m) {
    const p = this.place, several = series.length > 1;
    // A null between windows, so a line never joins across a break.
    const xs = [], cols = series.map(() => /** @type {(number|null)[]} */ ([]));
    p.x.forEach((x, i) => {
      if (i && Math.floor(x / (1 + GAP)) !== Math.floor(p.x[i - 1] / (1 + GAP))) { xs.push((p.x[i - 1] + x) / 2); cols.forEach((c) => c.push(null)); }
      xs.push(x);
      series.forEach((s, k) => cols[k].push(s.v[i]));
    });
    this.xs = xs; this.series = series;
    const sig = `${series.map((s) => s.t.id).join()}|${this.metric}|${this.lanes}|${css('--surface')}`;
    if (sig === this.sig && this.u) { this.u.setData([xs, ...cols], false); this.u.setScale('x', { min: this.view()[0], max: this.view()[1] }); this.drawLanes(); return; }
    this.u?.destroy();
    this.sig = sig;
    const self = this;
    const ax = { stroke: css('--ink-2'), font: FONT, grid: { stroke: css('--grid'), width: 1 }, ticks: { stroke: css('--axis'), width: 1, size: 4 } };
    const u = new uPlot({
      width: this.width(), height: 220, padding: [6, 8, 0, 0], legend: { show: false },
      scales: { x: { time: false, range: () => self.view() }, y: { range: () => [0, self.yTop] } },
      axes: [{ ...ax, splits: () => self.ticks().map((t) => t.x), values: () => self.ticks().map((t) => t.label) },
        { ...ax, size: this.lanes ? 96 : 52, values: (/** @type {any} */ _, /** @type {number[]} */ v) => v.map((x) => `${x}`), label: m.unit, labelSize: 14, labelFont: FONT }],
      series: [{}, ...series.map(({ t }) => {
        const slot = this.slot(t), stroke = this.colour(t);
        return { label: this.o.name(t.id), stroke, width: 2, dash: several ? DASH[(slot - 1) % DASH.length] : undefined, spanGaps: false,
          points: { show: true, size: 5, fill: stroke, filter: (/** @type {any} */ uu, /** @type {number} */ si) => lone(uu.data[si]) } };
      })],
      cursor: {
        drag: { x: true, y: false, setScale: false }, points: { show: false },
        bind: { dblclick: () => () => { self.setZoom(null); return null; } },
      },
      hooks: {
        setSelect: [(/** @type {any} */ q) => {
          const { left, width } = q.select;
          if (!width) return;
          q.setSelect({ left: 0, width: 0, top: 0, height: 0 }, false);
          if (width >= 5) self.setZoom([q.posToVal(left, 'x'), Math.max(q.posToVal(left + width, 'x'), q.posToVal(left, 'x') + 0.05)]);
        }],
        setCursor: [(/** @type {any} */ q) => self.onCursor(q)],
        draw: [(/** @type {any} */ q) => { self.drawBreaks(q); if (!self.lanes) self.drawMarks(q); self.drawLanes(); }],
      },
    }, [xs, ...cols], this.plotEl);
    u.over.addEventListener('mouseleave', () => hideTip());
    this.u = u;
  }

  /** The x axis' labels: each window's day, and its hours when they fit. */
  ticks() {
    const p = this.place, [a, b] = this.view(), span = b - a, out = [];
    // Pixels per window: the plot's width, without the y axis beside it.
    const px = (this.u ? this.u.bbox.width / (devicePixelRatio || 1) : this.width() - 60) / span;
    for (const w of p.wins) {
      if (w.x1 < a || w.x0 > b) continue;
      const hours = (w.end - w.start) / 3600e3;
      // An hour's label is about 55 px wide: one every hour or two when they fit.
      const every = px / Math.max(1, hours) > 90 ? 1 : px / Math.max(1, hours) > 50 ? 2 : 0;
      // Where a time falls: as placeRounds puts a round of that time.
      const iv = this.c.campaign.interval_s * 1000, span = w.end - w.start + iv;
      const at = (/** @type {number} */ t) => w.x0 + (t - w.start + iv / 2) / span;
      // A window cut by the zoom: its day at the edge, with no time (the hours say it).
      // Too narrow for its day and time (a phone, many windows): the day alone.
      if (w.x0 >= a) out.push({ x: at(w.start), label: px > 110 ? `${dayShort(w.start)} ${clock(w.start)}` : dayShort(w.start) });
      else out.push({ x: a, label: dayShort(w.start) });
      if (every) for (let hr = every; hr < hours; hr += every) out.push({ x: at(w.start + hr * 3600e3), label: clock(w.start + hr * 3600e3) });
    }
    // A day label needs about 60 px: drop the ones that would sit on the one before.
    const kept = [];
    for (const t of out.filter((x) => x.x >= a && x.x <= b)) if (!kept.length || (t.x - kept[kept.length - 1].x) * px >= 60) kept.push(t);
    return kept;
  }

  /** Shades the hours left out between windows, with a dashed edge. @param {any} u */
  drawBreaks(u) {
    const ctx = u.ctx, b = u.bbox, dpr = devicePixelRatio || 1;
    ctx.save(); ctx.beginPath(); ctx.rect(b.left, b.top, b.width, b.height); ctx.clip();
    for (let k = 0; k + 1 < this.place.wins.length; k++) {
      const xa = u.valToPos(this.place.wins[k].x1, 'x', true), xb = u.valToPos(this.place.wins[k + 1].x0, 'x', true);
      ctx.fillStyle = css('--surface-2'); ctx.fillRect(xa, b.top, xb - xa, b.height);
      ctx.strokeStyle = css('--axis'); ctx.lineWidth = dpr; ctx.setLineDash([3 * dpr, 3 * dpr]);
      ctx.beginPath(); ctx.moveTo(xa, b.top); ctx.lineTo(xa, b.top + b.height); ctx.moveTo(xb, b.top); ctx.lineTo(xb, b.top + b.height); ctx.stroke();
    }
    ctx.restore();
  }

  /** One target: an × on the baseline for each round it failed. @param {any} u */
  drawMarks(u) {
    const s = this.series?.[0];
    if (!s) return;
    const dpr = devicePixelRatio || 1, y = u.bbox.top + u.bbox.height - 5 * dpr;
    const [a, b] = this.view();
    s.bad.forEach((bad, i) => { const x = this.place.x[i]; if (bad && x >= a && x <= b) cross(u.ctx, u.valToPos(x, 'x', true), y, css('--ink-2'), dpr); });
  }

  /** Several targets: each one's failures in its own named lane. */
  drawLanes() {
    const u = this.u, cv = this.lanesEl, series = this.series || [];
    cv.hidden = !this.lanes || !u;
    if (cv.hidden) return;
    const dpr = devicePixelRatio || 1, lane = 15, hgt = series.length * lane + 4;
    cv.width = Math.round(this.width() * dpr); cv.height = Math.round(hgt * dpr); cv.style.height = hgt + 'px';
    const ctx = /** @type {CanvasRenderingContext2D} */ (cv.getContext('2d'));
    ctx.clearRect(0, 0, cv.width, cv.height);
    ctx.font = FONT.replace('11px', `${11 * dpr}px`); ctx.textBaseline = 'middle';
    const left = u.bbox.left, right = u.bbox.left + u.bbox.width;
    series.forEach((s, k) => {
      const y = (k * lane + 9) * dpr;
      ctx.fillStyle = css('--muted'); ctx.textAlign = 'right';
      let name = this.o.name(s.t.id);
      while (name.length > 3 && ctx.measureText(name).width > left - 10 * dpr) name = name.slice(0, -2) + '…';
      ctx.fillText(name, left - 6 * dpr, y);
      ctx.strokeStyle = css('--grid'); ctx.lineWidth = dpr; ctx.setLineDash([]);
      ctx.beginPath(); ctx.moveTo(left, y); ctx.lineTo(right, y); ctx.stroke();
      const [a, b] = this.view();
      s.bad.forEach((bad, i) => { const x = this.place.x[i]; if (bad && x >= a && x <= b) cross(ctx, u.valToPos(x, 'x', true), y, this.colour(s.t), dpr); });
    });
  }

  /** The round under the pointer: each target's value, or its steps when one shows. @param {any} u */
  onCursor(u) {
    const series = this.series, left = u.cursor.left;
    // The plot still shows the last campaign while the next one's timings load.
    if (!series || left == null || left < 0 || !u.over.matches(':hover') || this.place.x.length !== this.c.rounds.length) return;
    // The nearest round (the x data has nulls between windows).
    const x = u.posToVal(left, 'x');
    let i = -1, bd = Infinity;
    this.place.x.forEach((v, k) => { const d = Math.abs(v - x); if (d < bd) { bd = d; i = k; } });
    if (i < 0 || Math.abs(u.valToPos(this.place.x[i], 'x') - left) > 20) { hideTip(); return; }
    const r = this.c.rounds[i], m = METRICS[this.metric];
    const nodes = [h('div', { class: 't', text: `Round ${r.seq != null ? r.seq + 1 : ''} · ${dayShort(r.start || r.due)} ${clock(r.start || r.due)}` })];
    for (const s of [...series].sort((a, b) => (b.v[i] ?? -1) - (a.v[i] ?? -1))) {
      const sw = h('span', { class: 'sw' });
      sw.style.background = this.colour(s.t);
      nodes.push(h('div', { class: 'row' }, sw, h('span', { class: 'k', text: this.o.name(s.t.id) }),
        s.bad[i] ? 'failed' : s.v[i] != null ? m.fmt(/** @type {number} */ (s.v[i])) : r.outcome === 'gap' ? 'fyisp wasn’t running' : r.outcome === 'skipped' ? 'didn’t run' : 'no result'));
    }
    if (series.length === 1 && this.metric === 'ttfb') {
      const tm = this.timings?.[series[0].t.id], k = tm ? tm.at.indexOf(r.start || r.due) : -1;
      if (tm && k >= 0) {
        nodes.push(h('div', { class: 'muted', text: `Name lookup ${fmtMs(tm.dns_ms[k])} ms · connecting ${fmtMs(tm.connect_ms[k])} ms · encryption ${tm.tls_ms[k] == null ? 'none' : `${fmtMs(tm.tls_ms[k])} ms`}` }),
          h('div', { class: 'muted', text: `The median of ${tm.n[k]} fresh ${tm.n[k] === 1 ? 'connection' : 'connections'}.` }));
      }
    }
    const rc = u.over.getBoundingClientRect();
    showTip(nodes, rc.left + left, rc.top + u.cursor.top);
  }

  /** Which targets to show. */
  async pick() {
    const all = this.candidates(), chosen = new Set(this.shown().map((/** @type {any} */ t) => t.id));
    const list = h('div', { class: 'sp-pick-list' }, h('fieldset', { class: 'sp-pick-group' }, h('legend', { text: 'Targets' }), ...all.map((/** @type {any} */ t) => {
      const id = `st-pick-${t.id.replace(/[^A-Za-z0-9_-]/g, '_')}`;
      return h('div', { class: 'sp-pick-row', 'data-find': this.o.name(t.id).toLowerCase() },
        h('input', { type: 'checkbox', id, value: t.id, checked: chosen.has(t.id), onchange: (/** @type {Event} */ e) => { if (/** @type {HTMLInputElement} */ (e.target).checked) chosen.add(t.id); else chosen.delete(t.id); } }),
        h('label', { for: id }, h('span', { class: 'sp-pick-name', text: this.o.name(t.id) })));
    })));
    const v = await dialog({ title: 'Targets to show', fields: [list], buttons: [{ label: 'Cancel', value: 'cancel' }, { label: 'Show', value: 'ok', primary: true }] });
    if (v !== 'ok') return;
    this.setOnly(chosen.size === all.length || !chosen.size ? [] : all.map((/** @type {any} */ t) => t.id).filter((/** @type {string} */ id) => chosen.has(id)));
  }
}

/** The indexes of values with no neighbour on either side (drawn as points). @param {(number|null)[]} a */
function lone(a) { const out = []; for (let i = 0; i < a.length; i++) if (a[i] != null && a[i - 1] == null && a[i + 1] == null) out.push(i); return out; }

