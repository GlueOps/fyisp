// fyisp dashboard: the target filter of the Overview and Charts. The text is
// a regular expression (case-insensitive, matched anywhere in a target's
// name, provider, city or country), so "ap-south-2|us-east" finds several at
// once. Matching runs in a worker (match.js) with a time limit: a pattern
// that takes too long is stopped, and the last result stays.
// @ts-check

/** The longest pattern taken, from the box or a link. */
export const MAX_PATTERN = 100;
/** A match still running after this long is stopped. */
const LIMIT_MS = 250;
/** A worker that hasn't loaded after this long is given up on (the text is then matched as it is). */
const LOAD_MS = 5000;

/**
 * The pattern as it is used: trimmed, capped, and without empty alternatives
 * at its ends ("a|b|" would otherwise match everything). An escaped bar at
 * the end ("a\|") stays.
 * @param {string|null} s
 */
export function cleanPattern(s) {
  return (s || '').trim().slice(0, MAX_PATTERN).replace(/^[|\s]+/, '').replace(/(\\?)[|\s]+$/, (_, esc) => (esc ? '\\|' : ''));
}

export class PatternFilter {
  /**
   * @param {{name:string,fields:string[]}[]} rows the targets and the text of each that a pattern is matched against
   * @param {() => void} onChange called when names or problem changed
   */
  constructor(rows, onChange) {
    this.rows = rows;
    this.onChange = onChange;
    /** The pattern in effect ('' none). */
    this.pattern = '';
    /** The names it matches; null without a pattern. @type {Set<string>|null} */
    this.names = null;
    /** Why the pattern typed last is not in effect. @type {''|'invalid'|'slow'} */
    this.problem = '';
    /** The match asked for and not yet answered. @type {{pattern:string,resolve:()=>void,timer:number,sent:boolean}|null} */
    this.pending = null;
    /** @type {Worker|null} */
    this.worker = null;
    this.ready = false;
    /** No worker here: the text is matched as it is, not as a pattern. */
    this.plain = false;
    this.spawn();
  }

  has(/** @type {string} */ name) { return !this.names || this.names.has(name); }

  /** Starts a worker; it loads while the user types. */
  spawn() {
    this.ready = false;
    try {
      const w = this.worker = new Worker('static/match.js');
      w.onmessage = (/** @type {MessageEvent<{ready?:boolean,hits?:number[]}>} */ e) => {
        if (w !== this.worker) return;
        if (e.data.ready) { this.ready = true; this.send(); return; }
        const p = this.pending;
        if (p && p.sent) this.end(p.pattern, new Set((e.data.hits || []).map((i) => this.rows[i].name)), '');
      };
      w.onerror = () => { if (w === this.worker && !this.ready) this.noWorker(); };
    } catch {
      this.worker = null;
      this.plain = true;
    }
  }

  noWorker() {
    this.worker?.terminate();
    this.worker = null;
    this.plain = true;
    this.send();
  }

  /**
   * Makes pattern the filter. The returned promise ends when it is in effect,
   * was refused (problem says why, and the last result stays) or was replaced.
   * @param {string} pattern already through cleanPattern
   * @returns {Promise<void>}
   */
  set(pattern) {
    const old = this.pending;
    if (old) {
      // A newer pattern replaces it; a match already running goes with its worker.
      this.pending = null;
      window.clearTimeout(old.timer);
      if (old.sent) this.kill();
      old.resolve();
    }
    if (!pattern) { this.apply('', null, ''); return Promise.resolve(); }
    if (pattern === this.pattern && !this.problem) return Promise.resolve();
    try {
      new RegExp(pattern, 'i'); // compiled only: it runs in the worker
    } catch {
      this.apply(this.pattern, this.names, 'invalid');
      return Promise.resolve();
    }
    return new Promise((resolve) => {
      const p = this.pending = { pattern, resolve, sent: false, timer: window.setTimeout(() => { if (this.pending === p && !p.sent) this.noWorker(); }, LOAD_MS) };
      this.send();
    });
  }

  /** Hands the pending match to the worker once it has loaded. The limit covers the matching, not the load. */
  send() {
    const p = this.pending;
    if (!p || p.sent) return;
    if (this.plain) {
      const q = p.pattern.toLowerCase();
      this.end(p.pattern, new Set(this.rows.filter((r) => r.fields.some((s) => s.toLowerCase().includes(q))).map((r) => r.name)), '');
      return;
    }
    if (!this.worker || !this.ready) return;
    window.clearTimeout(p.timer);
    p.sent = true;
    this.worker.postMessage({ pattern: p.pattern, rows: this.rows.map((r) => r.fields) });
    p.timer = window.setTimeout(() => { this.kill(); this.end(this.pattern, this.names, 'slow'); }, LIMIT_MS);
  }

  /** Stops the worker (and a match running in it) and starts the next one. */
  kill() {
    this.worker?.terminate();
    this.spawn();
  }

  /** @param {string} pattern @param {Set<string>|null} names @param {''|'invalid'|'slow'} problem */
  end(pattern, names, problem) {
    const p = this.pending;
    this.pending = null;
    if (p) window.clearTimeout(p.timer);
    this.apply(pattern, names, problem);
    p?.resolve();
  }

  /** @param {string} pattern @param {Set<string>|null} names @param {''|'invalid'|'slow'} problem */
  apply(pattern, names, problem) {
    this.pattern = pattern;
    this.names = names;
    this.problem = problem;
    this.onChange();
  }
}
