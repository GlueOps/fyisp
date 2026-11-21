// Details → Speed: speed over time (api/speed?part=series). Download and
// upload in Mbit/s on two panels that share the time axis: by default one
// line of every scheduled test's faster results, or a line for each of up
// to 8 regions or servers. With one target and few enough tests in the
// window, its tests too (api/speed?part=tests), each drawn as a point over
// the line. Its own range, separate from the heatmap's
// span; a drag zooms (the window is asked for again, finer), a double
// click or the Zoomed button zooms out. uPlot is loaded as a classic
// deferred script before this module runs (window.uPlot), as app.js uses it.
// Plain ES module, no build step.
// @ts-check

import { h, fetchJSON, css, showTip, hideTip, tzParam, fmtMbps, fmtStep, clock, dialog, smooth } from './core.js';
import { SHAPES, SHAPE_CHAR, mark } from './speedmap.js';

/** @typedef {import('./speed.js').SpeedSummary} SpeedSummary */
/** @typedef {import('./speed.js').SpeedServer} SpeedServer */
/** @typedef {{id:string,kind:string,label:string,usual:number,down_bps:(number|null)[],up_bps:(number|null)[],n:number[],up_n:number[],failed:number[],down_lb:number[],up_lb:number[]}} SpeedLine */
/** @typedef {{at:number,from:number,to:number,step:number,t:number[],end:number,down_max:number|null,up_max:number|null,series:SpeedLine[]}} SpeedSeries */
/** @typedef {{id:number,start:number,outcome:string,server:string,down_bps:number|null,up_bps:number|null,down_lb?:boolean,up_lb?:boolean,fallback?:boolean,on_demand?:boolean,capped_by_server?:boolean}} SpeedPoint */
/** @typedef {{id:string,kind:string,label:string,servers:Object<string,{label:string,provider:string}>,tests:SpeedPoint[],capped:boolean,complete_from:number}} SpeedPoints */

const uPlot = /** @type {any} */ (window).uPlot;
const FONT = '11px system-ui, -apple-system, "Segoe UI", sans-serif';
const RANGES = ['24h', '48h', '7d', '30d', '90d'];
const MAX_TARGETS = 8; // the palette's hues (--s1..--s8), as the server allows
// The shortest window a drag zooms to.
const MIN_ZOOM = { pooled: 12 * 3600e3, targets: 24 * 3600e3 };
// A second cue beside the colour, by slot. Never dots: those mean "at least".
const DASH = [undefined, [8, 4], [16, 5], [10, 4, 3, 4]];
const AT_LEAST = [1.5, 4];
// One target's tests are each drawn as a point while they stay apart: about
// 0.6 to a pixel of the plot (a server's 90 days on a wide screen; a region's
// becomes a band of marks well before that), and always this many.
const POINTS_PER_PX = 0.6, POINTS_MIN = 200;
const OUTCOME = /** @type {Object<string,string>} */ ({ busy: 'server busy', unreachable: 'didn’t answer', failed: 'didn’t finish' });

/** "Mon, Nov 16". */
function day(/** @type {number} */ ms) { return new Date(ms).toLocaleDateString([], { weekday: 'short', day: 'numeric', month: 'short' }); }
function when(/** @type {number} */ ms) { return `${day(ms)}, ${clock(ms)}`; }
/** A round axis top just above v (Mbit/s). */
export function niceCeil(/** @type {number} */ v) {
  if (!(v > 0)) return 1;
  const p = 10 ** Math.floor(Math.log10(v));
  return ([1, 1.2, 1.5, 2, 2.5, 3, 4, 5, 6, 8, 10].find((k) => k * p >= v) || 10) * p;
}

/**
 * Shows the rows of a picker's list (.sp-pick-group > .sp-pick-row[data-find])
 * that hold q; reports whether any does.
 * @param {HTMLElement} list @param {string} q
 */
export function filterPick(list, q) {
  q = q.trim().toLowerCase();
  let any = false;
  for (const g of /** @type {NodeListOf<HTMLElement>} */ (list.querySelectorAll('.sp-pick-group'))) {
    let shown = 0;
    for (const r of /** @type {NodeListOf<HTMLElement>} */ (g.querySelectorAll('.sp-pick-row'))) {
      r.hidden = !!q && !(r.dataset.find || '').includes(q);
      if (!r.hidden) shown++;
    }
    g.hidden = !shown;
    any = any || shown > 0;
  }
  return any;
}

export class SpeedTime {
  constructor() {
    this.range = '7d';
    /** @type {[number, number]|null} a zoomed window (ms), within the range */
    this.zoom = null;
    /** @type {string[]} region or server ids; none: every test pooled */
    this.targets = [];
    /** @type {Map<string, number>} each target's colour slot, kept while it stays chosen */
    this.slots = new Map();
    /** @type {SpeedSummary|null} */
    this.d = null;
    /** @type {SpeedSeries|null} */
    this.data = null;
    /** @type {SpeedPoints|null} one target's tests, when each is drawn as a point */
    this.tests = null;
    // The fastest tests (bps) of the unzoomed range: the axes hold while zooming.
    this.top = { down: 0, up: 0 };
    this.yTop = { down: 1, up: 1 };
    this.view = [0, 1];
    this.latest = 0;
    this.err = '';
    /** @type {AbortController|null} */
    this.ctl = null;
    /** @type {any[]} the download and upload plots */
    this.plots = [];
    this.sig = '';
    this.lanes = false;
    this.focus = '';

    this.titleEl = h('h2', { id: 'spt-h', text: 'Speed over time' });
    this.pickBtn = h('button', { class: 'btn', type: 'button', 'aria-haspopup': 'dialog', onclick: () => this.pick() });
    this.rangeBtns = RANGES.map((r) => h('button', { type: 'button', text: r, onclick: () => this.setRange(r) }));
    this.zoomBtn = h('button', { class: 'btn sm spt-zoom', type: 'button', hidden: true, onclick: () => { this.zoomTo(null); this.rangeBtns[RANGES.indexOf(this.range)].focus(); } });
    this.legendEl = h('div', { class: 'spt-legend' });
    this.downEl = h('div', { class: 'spt-plot' });
    this.upEl = h('div', { class: 'spt-plot' });
    this.lanesEl = /** @type {HTMLCanvasElement} */ (h('canvas', { class: 'spt-lanes', hidden: true }));
    this.chartEl = h('div', { class: 'spt-chart', role: 'img' },
      h('p', { class: 'spt-axis', text: 'Download, Mbit/s' }), this.downEl,
      h('p', { class: 'spt-axis', text: 'Upload, Mbit/s' }), this.upEl, this.lanesEl);
    this.emptyEl = h('p', { class: 'muted', hidden: true });
    this.cornerEl = h('p', { class: 'spt-corner muted small' });
    this.capEl = h('p', { class: 'spt-cap' });
    this.say = h('p', { class: 'sr-only', role: 'status' });
    this.el = h('section', { class: 'spt', 'aria-labelledby': 'spt-h' },
      h('div', { class: 'sp-sec-head' }, this.titleEl,
        h('div', { class: 'sp-ctl' }, this.pickBtn, h('div', { class: 'seg sm', role: 'group', 'aria-label': 'Range' }, ...this.rangeBtns), this.zoomBtn)),
      this.legendEl, this.emptyEl, this.chartEl, this.cornerEl, this.capEl, this.say);
    // The section, not the chart: the chart is hidden while there is nothing to draw.
    new ResizeObserver(() => this.resize()).observe(this.el);
  }

  /** The page's summary arrived: read the series again when a new test has landed. @param {SpeedSummary} d */
  refresh(d) {
    this.d = d;
    const latest = d.latest ? d.latest.id : 0;
    if (!this.data || this.err || (latest !== this.latest && !this.zoom)) { this.latest = latest; this.load(); } else this.renderControls();
  }

  /** The plots' width: the chart's, or the section's inside its padding while the chart is hidden. */
  width() { return Math.max(200, this.chartEl.clientWidth || this.el.clientWidth - 34); }

  async load() {
    if (!this.el.clientWidth) return; // Speed isn't showing: read when it does (resize)
    this.ctl?.abort();
    const ctl = this.ctl = new AbortController();
    const q = new URLSearchParams({ part: 'series', tz: tzParam(), points: String(Math.round(this.width())) });
    if (this.zoom) { q.set('from', String(Math.round(this.zoom[0]))); q.set('to', String(Math.round(this.zoom[1]))); } else q.set('from', 'now-' + this.range);
    if (this.targets.length) q.set('targets', this.targets.join(','));
    try {
      const d = /** @type {SpeedSeries} */ (await fetchJSON('api/speed?' + q, { signal: ctl.signal }));
      if (ctl.signal.aborted) return;
      // One target with few enough tests in the window: each of them, too.
      const l = d.series[0], sum = (/** @type {number} */ a, /** @type {number} */ b) => a + b;
      /** @type {SpeedPoints|null} */
      let tests = null;
      if (d.series.length === 1 && l.kind !== 'pooled' && l.n.reduce(sum, 0) + l.failed.reduce(sum, 0) <= Math.max(POINTS_MIN, POINTS_PER_PX * this.width())) {
        tests = await fetchJSON('api/speed?' + new URLSearchParams({ part: 'tests', row: l.id, from: String(d.from), to: String(d.to) }), { signal: ctl.signal });
        if (ctl.signal.aborted) return;
      }
      this.data = d;
      this.tests = tests;
      this.err = '';
      // The axes hold while zooming: the range's fastest test, an asked-for one among the points included.
      const most = (/** @type {'down_bps'|'up_bps'} */ k, /** @type {number|null} */ top) => Math.max(top || 0, ...(tests ? tests.tests.map((t) => t[k] || 0) : []));
      if (!this.zoom) this.top = { down: most('down_bps', d.down_max), up: most('up_bps', d.up_max) };
    } catch (e) {
      if (ctl.signal.aborted) return;
      // A chosen server a release removed: back to every test.
      if (this.targets.length && /** @type {any} */ (e)?.status === 404) { this.setTargets([]); return; }
      this.err = 'Couldn’t load the speed tests over time. They’ll show again when fyisp answers.';
    }
    this.render();
  }

  /** @param {string} r */
  setRange(r) {
    if (r === this.range && !this.zoom && !this.err) return;
    this.range = r;
    this.zoom = null;
    this.load();
  }

  /** @param {string[]} ids */
  setTargets(ids) {
    const slots = new Map();
    for (const id of ids) if (this.slots.has(id)) slots.set(id, this.slots.get(id));
    for (const id of ids) {
      if (slots.has(id)) continue;
      let s = 1;
      while ([...slots.values()].includes(s)) s++;
      slots.set(id, s);
    }
    this.targets = ids;
    this.slots = slots;
    this.zoom = null;
    this.load();
  }

  /** @param {[number, number]|null} z */
  zoomTo(z) {
    const d = this.data;
    if (z && d) {
      const min = this.targets.length ? MIN_ZOOM.targets : MIN_ZOOM.pooled;
      let [a, b] = z;
      if (b - a < min) { const mid = (a + b) / 2; a = mid - min / 2; b = mid + min / 2; }
      if (a < d.from) { b += d.from - a; a = d.from; }
      if (b > d.to) { a -= b - d.to; b = d.to; }
      z = [Math.max(d.from, a), b];
      if (z[1] - z[0] > 0.95 * (d.to - d.from)) return; // nothing to zoom into
      // The two plots are synced: both report the same drag.
      if (this.zoom && Math.abs(this.zoom[0] - z[0]) < 1000 && Math.abs(this.zoom[1] - z[1]) < 1000) return;
    }
    if (!z && !this.zoom) return;
    this.zoom = z;
    this.say.textContent = z ? `Zoomed to ${when(z[0])} to ${when(z[1])}.` : 'Zoomed out.';
    this.load();
  }

  /** The line's name: a target's label, or every test's. @param {SpeedLine} l */
  name(l) { return l.kind === 'pooled' ? 'Your faster tests' : l.label; }

  pooledWord() { return this.d && this.d.mode !== 'regions' ? 'All servers' : 'All regions'; }

  renderControls() {
    const n = this.targets.length, d = this.data;
    // One target: its label, once it has loaded.
    const one = n === 1 && d && d.series[0].id === this.targets[0] ? d.series[0].label : '1 target';
    const what = n === 0 ? this.pooledWord() : n === 1 ? one : `${n} targets`;
    this.titleEl.textContent = `Speed over time: ${n === 0 ? what.toLowerCase() : what}`;
    this.pickBtn.textContent = `Tests to: ${what} ▾`;
    this.pickBtn.disabled = !this.d;
    this.rangeBtns.forEach((b, i) => b.setAttribute('aria-pressed', String(RANGES[i] === this.range && !this.zoom)));
    this.zoomBtn.hidden = !this.zoom;
    if (this.zoom) this.zoomBtn.textContent = `Zoomed: ${when(this.zoom[0])} to ${when(this.zoom[1])} ×`;
  }

  render() {
    this.renderControls();
    const d = this.data;
    const any = !!d && !this.err && d.series.some((l) => l.n.some((x) => x > 0) || l.failed.some((x) => x > 0));
    this.emptyEl.hidden = any;
    this.emptyEl.textContent = this.err || (this.zoom ? 'No tests in this window.' : 'No scheduled tests in this range yet. Test now and “Test every server” results are in the tests below.');
    this.chartEl.hidden = !any;
    if (!d || !any) { this.legendEl.replaceChildren(); this.cornerEl.textContent = this.capEl.textContent = ''; return; }
    const pooled = d.series[0].kind === 'pooled';
    const several = d.series.length > 1;
    const daily = d.step >= 86400e3;
    const stepWord = daily ? '1 day' : fmtStep(d.step).replace('h', ' h');
    const per = daily ? 'day' : stepWord.replace(' h', ' hours');
    const T = this.tests;
    this.cornerEl.textContent = several ? `${d.series.length} targets · ${stepWord} per point`
      : T ? `each point is one test · ${T.capped ? 'the newest ' : ''}${T.tests.length} ${T.tests.length === 1 ? 'test' : 'tests'}`
        : `${stepWord} per point · about ${d.series[0].usual} tests each${daily ? ' · slow evenings are averaged into the day' : ''}${pooled ? '' : ' · zoom in to see each test'}`;
    const lb = !pooled && d.series.some((l) => l.down_lb.some((x, i) => x * 2 > l.n[i]) || l.up_lb.some((x, i) => x * 2 > l.up_n[i]));
    this.capEl.textContent = (pooled
      ? `Your faster tests in each ${per}, mostly to the nearest servers. It is not your plan’s speed, and it reads higher than Home’s usual speed, which counts every test. Watch for changes.`
      : several ? 'Each line is the typical test to that target. Far servers are slower: that is distance, not a fault. Compare each line with its own past.'
        : T ? `Every test to this target, Test now and “Test every server” included; the thin line is the typical scheduled test of each ${per}.`
          : 'The typical test to this target.')
      + (lb && !T ? ' Dotted: most tests were still speeding up when they ended, so the speed was at least this.' : '');
    this.chartEl.setAttribute('aria-label', this.summary(d) + (T ? ` ${T.tests.length} tests, one point each.` : ''));
    this.lanes = several && d.series.some((l) => this.failedAt(l).some(Boolean));
    this.renderLegend(d);
    this.draw(d);
  }

  /** One sentence a line for a screen reader: the usual level and the lowest point. @param {SpeedSeries} d */
  summary(d) {
    return d.series.map((l) => {
      const pts = l.down_bps.map((v, i) => ({ v, i })).filter((p) => p.v != null);
      if (!pts.length) return `${this.name(l)}: no finished tests.`;
      const sorted = pts.map((p) => /** @type {number} */ (p.v)).sort((a, b) => a - b);
      const low = pts.reduce((a, b) => (/** @type {number} */ (b.v) < /** @type {number} */ (a.v) ? b : a));
      return `${this.name(l)}: download usually about ${fmtMbps(sorted[sorted.length >> 1])}, lowest about ${fmtMbps(low.v)} around ${when(d.t[low.i])}.`;
    }).join(' ');
  }

  /** @param {SpeedLine} l */
  slot(l) { return l.kind === 'pooled' ? 1 : this.slots.get(l.id) || 1; }

  /** The line's dash, when several show. @param {SpeedLine} l @param {boolean} several */
  dash(l, several) { return several ? DASH[(this.slot(l) - 1) % DASH.length] : undefined; }

  /** A legend swatch: the line as the plot draws it. @param {SpeedLine} l @param {boolean} several */
  swatch(l, several) { return swatch(css('--s' + this.slot(l)), this.dash(l, several)); }

  /** @param {SpeedSeries} d */
  renderLegend(d) {
    const several = d.series.length > 1;
    this.focus = ''; // the entry that set it may be gone without a mouseleave or a blur
    const kids = d.series.map((l) => {
      const lbMost = l.down_lb.reduce((a, b) => a + b, 0) * 3 > l.n.reduce((a, b) => a + b, 0);
      const none = !l.n.some((x) => x > 0) && !l.failed.some((x) => x > 0);
      const kidsOf = [this.swatch(l, several), this.name(l),
        none ? h('span', { class: 'spt-tag', text: 'no scheduled tests' }) : l.kind !== 'pooled' && lbMost ? h('span', { class: 'spt-tag', text: 'often “at least”' }) : null];
      if (!several) return h('span', { class: 'spt-key' }, ...kidsOf);
      // Showing one alone replaces this button: the picker's takes the focus.
      const b = h('button', { type: 'button', class: 'spt-key', title: 'Show this one alone', onclick: () => { this.setTargets([l.id]); this.pickBtn.focus(); } }, ...kidsOf);
      const on = () => { this.focus = l.id; this.applyFocus(); }, off = () => { this.focus = ''; this.applyFocus(); };
      b.addEventListener('mouseenter', on); b.addEventListener('mouseleave', off);
      b.addEventListener('focus', on); b.addEventListener('blur', off);
      return b;
    });
    const T = this.tests;
    if (T) {
      // Each test's mark: a shape for its server, as the row's chart by hour of day has them.
      const col = css('--s' + this.slot(d.series[0]));
      const glyph = (/** @type {string} */ g) => { const e = h('span', { 'aria-hidden': 'true', text: g }); e.style.color = col; return e; };
      for (const [id, shape] of this.shapes(T)) kids.push(h('span', { class: 'spt-key' }, glyph(SHAPE_CHAR[/** @type {'circle'} */ (shape)]), T.servers[id]?.label || id));
      if (T.tests.some((t) => t.fallback)) kids.push(h('span', { class: 'spt-key muted' }, glyph('○'), 'hollow: a backup server ran it'));
      if (T.tests.some((t) => t.down_lb || t.up_lb)) kids.push(h('span', { class: 'spt-key muted' }, glyph('↑'), 'at least'));
      if (T.tests.some((t) => t.on_demand)) kids.push(h('span', { class: 'spt-key muted' }, this.ringSwatch(), 'ringed: you asked for it'));
    }
    kids.push(h('span', { class: 'spt-key muted' }, h('span', { 'aria-hidden': 'true', text: '×' }), T ? 'didn’t finish' : 'tests ran, most didn’t finish'));
    this.legendEl.replaceChildren(...kids);
  }

  /** Dims every line but the one a legend entry points at. */
  applyFocus() {
    const d = this.data;
    if (!d) return;
    for (const u of this.plots) {
      // Behind each test's own point the line steps back.
      d.series.forEach((l, k) => { for (const si of [1 + 2 * k, 2 + 2 * k]) u.series[si].alpha = this.focus && this.focus !== l.id ? 0.2 : this.tests ? 0.5 : 1; });
      u.redraw(false);
    }
  }

  /** The plots' colours are fixed when they are made. */
  themeChanged() { this.sig = ''; if (this.data) this.render(); }

  resize() {
    if (!this.el.clientWidth) return;
    if ((!this.data || this.err) && this.d) { this.load(); return; }
    for (const u of this.plots) if (Math.abs(u.width - this.width()) > 1) u.setSize({ width: this.width(), height: u.height });
  }

  /**
   * Each line is two series: its own, and a dotted one where most of a
   * point's tests were lower bounds (uPlot has no dash per segment). A
   * dotted stretch shares its end points with the other.
   * @param {SpeedSeries} d @param {'down'|'up'} dir
   */
  columns(d, dir) {
    const xs = d.t.map((t, i) => (t + (i + 1 < d.t.length ? d.t[i + 1] : d.end)) / 2000);
    const cols = [xs];
    for (const l of d.series) {
      const v = dir === 'down' ? l.down_bps : l.up_bps, n = dir === 'down' ? l.n : l.up_n, lb = dir === 'down' ? l.down_lb : l.up_lb;
      const dot = v.map((x, i) => l.kind !== 'pooled' && x != null && lb[i] * 2 > n[i]);
      cols.push(v.map((x, i) => (x == null || dot[i] ? null : x / 1e6)));
      cols.push(v.map((x, i) => (x != null && (dot[i] || (dot[i - 1] && v[i - 1] != null) || (dot[i + 1] && v[i + 1] != null)) ? x / 1e6 : null)));
    }
    return cols;
  }

  /** @param {SpeedSeries} d */
  draw(d) {
    const T = this.tests;
    const sig = `${d.series.map((l) => l.id + ':' + this.slot(l)).join()}|${this.lanes}|${!!T}|${css('--surface')}`;
    this.view = [d.from / 1000, d.to / 1000];
    // A zoom may hold faster tests than the range showed as a line.
    const most = (/** @type {'down_bps'|'up_bps'} */ k, /** @type {number} */ top) => Math.max(top, ...(T ? T.tests.map((t) => t[k] || 0) : []));
    this.yTop = { down: niceCeil(most('down_bps', this.top.down) * 1.03 / 1e6), up: niceCeil(most('up_bps', this.top.up) * 1.03 / 1e6) };
    if (sig !== this.sig) {
      for (const u of this.plots) u.destroy();
      this.sig = sig;
      this.plots = [this.plot(d, 'down', this.downEl, 170), this.plot(d, 'up', this.upEl, 110)];
    } else {
      this.plots[0].setData(this.columns(d, 'down'), false);
      this.plots[1].setData(this.columns(d, 'up'), false);
      for (const u of this.plots) u.setScale('x', { min: this.view[0], max: this.view[1] });
    }
    this.applyFocus();
    this.drawLanes();
  }

  /** @param {SpeedSeries} d @param {'down'|'up'} dir @param {HTMLElement} el @param {number} height */
  plot(d, dir, el, height) {
    const self = this;
    const several = d.series.length > 1;
    const ax = { stroke: css('--ink-2'), font: FONT, grid: { stroke: css('--grid'), width: 1 }, ticks: { stroke: css('--axis'), width: 1, size: 4 } };
    /** A value with no neighbour on either side still shows, as a point. */
    const lone = (/** @type {any} */ u, /** @type {number} */ si) => {
      const a = u.data[si], b = u.data[si % 2 ? si + 1 : si - 1], out = [];
      if (self.tests) return out; // each test has its own point
      for (let i = 0; i < a.length; i++) if (a[i] != null && a[i - 1] == null && a[i + 1] == null && (b[i] == null || (b[i - 1] == null && b[i + 1] == null))) out.push(i);
      return out;
    };
    const series = [{}];
    for (const l of d.series) {
      const stroke = css('--s' + this.slot(l));
      const base = { stroke, width: this.tests ? 1 : 2, spanGaps: false, points: { show: true, size: 6, fill: stroke, filter: lone } };
      series.push({ ...base, label: this.name(l), dash: this.dash(l, several) }, { ...base, label: this.name(l) + ' (at least)', dash: AT_LEAST });
    }
    const u = new uPlot({
      width: this.width(), height, padding: [6, 8, 0, 0], legend: { show: false },
      scales: { x: { time: true, range: () => self.view }, y: { range: () => [0, self.yTop[dir]] } },
      // Wider beside the lanes: their names go there.
      axes: [ax, { ...ax, size: this.lanes ? 96 : 48 }],
      series,
      cursor: {
        sync: { key: 'speedtime', setSeries: false },
        drag: { x: true, y: false, setScale: false },
        points: { show: false },
        bind: { dblclick: () => () => { self.zoomTo(null); return null; } },
      },
      hooks: {
        setSelect: [(/** @type {any} */ p) => {
          const { left, width } = p.select;
          if (!width) return;
          p.setSelect({ left: 0, width: 0, top: 0, height: 0 }, false);
          if (width >= 5) self.zoomTo([p.posToVal(left, 'x') * 1000, p.posToVal(left + width, 'x') * 1000]);
        }],
        setCursor: [(/** @type {any} */ p) => (self.tests ? self.onTest(p, dir) : self.onCursor(p))],
        draw: [(/** @type {any} */ p) => { if (self.tests) self.drawTests(p, dir); else self.drawMarks(p); if (dir === 'up') self.drawLanes(); }],
      },
    }, this.columns(d, dir), el);
    u.over.addEventListener('mouseleave', () => hideTip());
    return u;
  }

  /** Points where tests ran and most didn't finish: an × on the baseline (one line), or in its lane (several). @param {SpeedLine} l */
  failedAt(l) { return l.failed.map((f, i) => f > 0 && f >= l.n[i]); }

  /** @param {any} u */
  drawMarks(u) {
    const d = this.data;
    if (!d || d.series.length !== 1) return;
    const xs = u.data[0], dpr = devicePixelRatio || 1, y = u.bbox.top + u.bbox.height - 5 * dpr;
    this.failedAt(d.series[0]).forEach((bad, i) => { if (bad) cross(u.ctx, u.valToPos(xs[i], 'x', true), y, css('--ink-2'), dpr); });
  }

  /**
   * Each server's shape: in its region's server order, then as the tests
   * use them, as the selected row's chart by hour of day gives them out, so
   * a shape is the same server in both and at every range.
   * @param {SpeedPoints} T
   */
  shapes(T) {
    const chain = (T.kind === 'region' && this.d?.regions.find((r) => r.id === T.id)?.servers) || [];
    const ids = [...new Set([...chain, ...T.tests.map((t) => t.server)])];
    return new Map(ids.map((id, i) => [id, SHAPES[i % SHAPES.length]]).filter(([id]) => T.tests.some((t) => t.server === id)));
  }

  /** The legend's mark for a test the owner asked for: a point in a ring, as the chart draws it. */
  ringSwatch() {
    const dpr = devicePixelRatio || 1, d = this.data;
    const cv = /** @type {HTMLCanvasElement} */ (h('canvas', { class: 'spt-ring', width: String(Math.round(16 * dpr)), height: String(Math.round(16 * dpr)), 'aria-hidden': 'true' }));
    const ctx = /** @type {CanvasRenderingContext2D} */ (cv.getContext('2d'));
    ctx.scale(dpr, dpr);
    mark(ctx, 'circle', 8, 8, css('--s' + (d ? this.slot(d.series[0]) : 1)), false);
    ctx.strokeStyle = css('--ink-2'); ctx.lineWidth = 1; ctx.beginPath(); ctx.arc(8, 8, 7, 0, 2 * Math.PI); ctx.stroke();
    return cv;
  }

  /**
   * One target's tests, each as its own point: its server's shape, hollow
   * on a backup server, an up-tick on a lower bound, a ring on one the
   * owner asked for, and an × on the baseline when the direction has no
   * result. Smaller when there are many.
   * @param {any} u @param {'down'|'up'} dir
   */
  drawTests(u, dir) {
    const T = this.tests, d = this.data;
    if (!T || !d) return;
    const ctx = /** @type {CanvasRenderingContext2D} */ (u.ctx), b = u.bbox, dpr = devicePixelRatio || 1;
    const k = (T.tests.length > 400 ? 0.62 : 1) * dpr, col = css('--s' + this.slot(d.series[0])), shapes = this.shapes(T);
    ctx.save();
    ctx.beginPath(); ctx.rect(b.left - 8 * dpr, b.top - 14 * dpr, b.width + 16 * dpr, b.height + 16 * dpr); ctx.clip();
    for (const t of T.tests) {
      const x = u.valToPos(t.start / 1000, 'x', true), v = dir === 'down' ? t.down_bps : t.up_bps;
      if (v == null) { cross(ctx, x, b.top + b.height - 5 * dpr, css('--ink-2'), dpr); continue; }
      ctx.save();
      ctx.translate(x, u.valToPos(v / 1e6, 'y', true)); ctx.scale(k, k); ctx.setLineDash([]);
      mark(ctx, shapes.get(t.server) || 'circle', 0, 0, col, !!t.fallback);
      if (dir === 'down' ? t.down_lb : t.up_lb) { ctx.strokeStyle = col; ctx.lineWidth = 1.4; ctx.beginPath(); ctx.moveTo(0, -6); ctx.lineTo(0, -12); ctx.moveTo(-2.5, -9.5); ctx.lineTo(0, -12); ctx.lineTo(2.5, -9.5); ctx.stroke(); }
      if (t.on_demand) { ctx.strokeStyle = css('--ink-2'); ctx.lineWidth = 1; ctx.beginPath(); ctx.arc(0, 0, 7, 0, 2 * Math.PI); ctx.stroke(); }
      ctx.restore();
    }
    ctx.restore();
  }

  /** The test nearest the pointer. @param {any} u @param {'down'|'up'} dir */
  onTest(u, dir) {
    const T = this.tests;
    if (!T || u.cursor.left < 0 || !u.over.matches(':hover')) return;
    let best = null, bd = 30;
    for (const t of T.tests) {
      const v = dir === 'down' ? t.down_bps : t.up_bps;
      const dd = Math.hypot(u.valToPos(t.start / 1000, 'x') - u.cursor.left, (v == null ? u.over.clientHeight - 5 : u.valToPos(v / 1e6, 'y')) - u.cursor.top);
      if (dd < bd) { bd = dd; best = t; }
    }
    if (!best) { hideTip(); return; }
    const t = best, sv = T.servers[t.server];
    const flags = [t.fallback ? 'a backup server' : '', t.on_demand ? 'you asked for it' : '', t.capped_by_server ? 'as fast as the server goes' : ''].filter(Boolean);
    const r = u.over.getBoundingClientRect();
    showTip([
      h('div', { class: 't', text: when(t.start) }),
      h('div', { text: `${sv ? sv.label : t.server}${flags.length ? ` (${flags.join(', ')})` : ''}` }),
      h('div', { text: t.down_bps == null ? OUTCOME[t.outcome] || 'didn’t finish'
        : `${t.down_lb ? 'at least ' : ''}${fmtMbps(t.down_bps)} down · ${t.up_bps == null ? 'the upload didn’t finish' : `${t.up_lb ? 'at least ' : ''}${fmtMbps(t.up_bps)} up`}` }),
    ], r.left + u.cursor.left, r.top + u.cursor.top);
  }

  /** Shows one region or server alone (the Speed page's selected row). @param {string} id */
  show(id) {
    this.setTargets([id]);
    this.el.scrollIntoView({ behavior: smooth(), block: 'start' });
    this.pickBtn.focus({ preventScroll: true });
  }

  /** With several lines, each one's failures in its own named lane under the plots. */
  drawLanes() {
    const d = this.data, u = this.plots[1], cv = this.lanesEl;
    cv.hidden = !d || !this.lanes || !u;
    if (cv.hidden || !d) return;
    const dpr = devicePixelRatio || 1, lane = 15, hgt = d.series.length * lane + 4;
    cv.width = Math.round(this.width() * dpr); cv.height = Math.round(hgt * dpr);
    cv.style.height = hgt + 'px';
    const ctx = /** @type {CanvasRenderingContext2D} */ (cv.getContext('2d'));
    ctx.clearRect(0, 0, cv.width, cv.height);
    ctx.font = FONT.replace('11px', `${11 * dpr}px`);
    ctx.textBaseline = 'middle';
    const left = u.bbox.left, right = u.bbox.left + u.bbox.width, xs = u.data[0];
    d.series.forEach((l, k) => {
      const y = (k * lane + 9) * dpr;
      ctx.globalAlpha = this.focus && this.focus !== l.id ? 0.3 : 1;
      ctx.fillStyle = css('--muted'); ctx.textAlign = 'right';
      let name = l.label;
      while (name.length > 3 && ctx.measureText(name).width > left - 10 * dpr) name = name.slice(0, -2) + '…';
      ctx.fillText(name, left - 6 * dpr, y);
      ctx.strokeStyle = css('--grid'); ctx.lineWidth = dpr; ctx.setLineDash([]);
      ctx.beginPath(); ctx.moveTo(left, y); ctx.lineTo(right, y); ctx.stroke();
      this.failedAt(l).forEach((bad, i) => { if (bad) cross(ctx, u.valToPos(xs[i], 'x', true), y, css('--s' + this.slot(l)), dpr); });
    });
    ctx.globalAlpha = 1;
  }

  /** @param {any} u */
  onCursor(u) {
    const d = this.data, i = u.cursor.idx;
    if (!d || i == null || u.cursor.left < 0 || !u.over.matches(':hover')) return;
    const end = i + 1 < d.t.length ? d.t[i + 1] : d.end;
    const nodes = [h('div', { class: 't', text: end - d.t[i] >= 86400e3 ? day(d.t[i]) : `${when(d.t[i])} to ${clock(end)}` })];
    for (const l of [...d.series].sort((a, b) => (b.down_bps[i] || 0) - (a.down_bps[i] || 0))) {
      const sw = h('span', { class: 'sw' });
      sw.style.background = css('--s' + this.slot(l));
      const atLeast = l.kind !== 'pooled' && l.down_lb[i] * 2 > l.n[i] ? 'at least ' : '';
      const val = l.down_bps[i] != null ? `${atLeast}${fmtMbps(l.down_bps[i])} down · ${fmtMbps(l.up_bps[i])} up`
        : l.n[i] ? 'too few tests to compare' : l.failed[i] ? 'none finished' : 'no tests';
      nodes.push(h('div', { class: 'row' }, sw, h('span', { class: 'k', text: this.name(l) }), val));
      nodes.push(h('div', { class: 'muted', text: `${l.n[i]} ${l.n[i] === 1 ? 'test' : 'tests'} finished${l.failed[i] ? `, ${l.failed[i]} didn’t` : ''}` }));
    }
    const r = u.over.getBoundingClientRect();
    showTip(nodes, r.left + u.cursor.left, r.top + u.cursor.top);
  }

  /** The picker: every test together, or up to 8 regions and servers. */
  async pick() {
    const d = this.d;
    if (!d) return;
    const byId = new Map(d.servers.map((s) => [s.id, s]));
    const inRegion = new Set(d.regions.flatMap((r) => r.servers));
    const item = (/** @type {SpeedServer} */ s, /** @type {string} */ group) => ({ id: s.id, label: s.label, find: `${s.label} ${s.provider} ${s.city} ${group}` });
    /** A region's group starts with the region itself; its servers are set in under it. @type {{title:string,region:boolean,items:{id:string,label:string,find:string}[]}[]} */
    const groups = d.regions.map((rg) => ({
      title: rg.title, region: true,
      items: [{ id: rg.id, label: `${rg.title}: every server`, find: rg.title },
        ...rg.servers.map((id) => byId.get(id)).filter((s) => !!s).map((s) => item(/** @type {SpeedServer} */ (s), rg.title))],
    }));
    const other = d.servers.filter((s) => !inRegion.has(s.id));
    if (other.length) groups.push({ title: d.regions.length ? 'Other servers' : 'Servers', region: false, items: other.map((s) => item(s, '')) });
    const chosen = new Set(this.targets);
    const all = /** @type {HTMLInputElement} */ (h('input', { type: 'checkbox', id: 'spt-all', checked: !chosen.size }));
    const count = h('span', { class: 'muted small', role: 'status' });
    const search = /** @type {HTMLInputElement} */ (h('input', { type: 'search', id: 'spt-find', placeholder: 'City, country or provider', autocomplete: 'off' }));
    const list = h('div', { class: 'sp-pick-list' });
    const none = h('p', { class: 'muted', text: 'Nothing matches.', hidden: true });
    const sync = () => {
      all.checked = !chosen.size;
      count.textContent = chosen.size ? `${chosen.size} of ${MAX_TARGETS} chosen` : `Or pick up to ${MAX_TARGETS}:`;
      for (const b of /** @type {NodeListOf<HTMLInputElement>} */ (list.querySelectorAll('input'))) { b.checked = chosen.has(b.value); b.disabled = !b.checked && chosen.size >= MAX_TARGETS; }
    };
    for (const g of groups) {
      list.append(h('fieldset', { class: 'sp-pick-group' }, h('legend', { text: g.title }), ...g.items.map((it, k) => {
        const id = `spt-pick-${it.id.replace(/[^A-Za-z0-9_-]/g, '_')}`;
        const box = h('input', { type: 'checkbox', id, value: it.id, onchange: (/** @type {Event} */ e) => { if (/** @type {HTMLInputElement} */ (e.target).checked) chosen.add(it.id); else chosen.delete(it.id); sync(); } });
        return h('div', { class: `sp-pick-row${g.region && k ? ' spt-sub' : ''}`, 'data-find': it.find.toLowerCase() }, box, h('label', { for: id }, h('span', { class: 'sp-pick-name', text: it.label })));
      })));
    }
    all.addEventListener('change', () => { if (all.checked) chosen.clear(); sync(); });
    search.addEventListener('input', () => { none.hidden = filterPick(list, search.value); });
    sync();
    const v = await dialog({
      title: 'Tests to show',
      fields: [
        h('div', { class: 'sp-pick-row' }, all, h('label', { for: 'spt-all' }, h('span', { class: 'sp-pick-name', text: `${this.pooledWord()} together (one line)` }))),
        count, h('label', { class: 'fld', for: 'spt-find' }, h('span', { text: 'Find' }), search), list, none],
      buttons: [{ label: 'Cancel', value: 'cancel' }, { label: 'Show', value: 'ok', primary: true }],
      focus: search,
    });
    if (v !== 'ok') return;
    // In the order of the list, so the lines and their colours don't depend on the order of ticking.
    this.setTargets(groups.flatMap((g) => g.items.map((it) => it.id)).filter((id) => chosen.has(id)));
  }
}

/** A legend's mark for a line: a short stroke in its colour and dash. @param {string} colour @param {number[]|undefined} dash */
export function swatch(colour, dash) {
  const dpr = devicePixelRatio || 1;
  const cv = /** @type {HTMLCanvasElement} */ (h('canvas', { class: 'spt-sw', width: String(Math.round(30 * dpr)), height: String(Math.round(8 * dpr)) }));
  const ctx = /** @type {CanvasRenderingContext2D} */ (cv.getContext('2d'));
  ctx.strokeStyle = colour;
  ctx.lineWidth = 2.5 * dpr;
  ctx.setLineDash((dash || []).map((x) => x * dpr * 0.75));
  ctx.beginPath(); ctx.moveTo(0, 4 * dpr); ctx.lineTo(30 * dpr, 4 * dpr); ctx.stroke();
  return cv;
}

/** @param {CanvasRenderingContext2D} ctx @param {number} x @param {number} y @param {string} colour @param {number} dpr */
export function cross(ctx, x, y, colour, dpr) {
  const r = 3.5 * dpr;
  ctx.strokeStyle = colour; ctx.lineWidth = 1.6 * dpr; ctx.setLineDash([]);
  ctx.beginPath(); ctx.moveTo(x - r, y - r); ctx.lineTo(x + r, y + r); ctx.moveTo(x + r, y - r); ctx.lineTo(x - r, y + r); ctx.stroke();
}
