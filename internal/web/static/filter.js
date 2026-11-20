// fyisp dashboard: the target filter of the Overview and Charts. The text is
// a regular expression (case-insensitive, matched anywhere in a target's
// name, provider, city or country), so "ap-south-2|us-east" finds several at
// once. Matching runs in a worker (match.js) with a time limit: a pattern
// that takes too long is stopped, and the last result stays.
// @ts-check

/** The longest pattern taken, from the box or a link. */
export const MAX_PATTERN = 100;
/** A pattern still matching after this long is stopped. */
const LIMIT_MS = 250;

/**
 * The pattern as it is used: trimmed, capped, and without empty alternatives
 * at its ends ("a|b|" would otherwise match everything).
 * @param {string|null} s
 */
export function cleanPattern(s) {
  return (s || '').trim().slice(0, MAX_PATTERN).replace(/^\|+|\|+$/g, '').trim();
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
    this.seq = 0;
    /** The pattern being matched. */
    this.want = '';
    /** @type {Worker|null} */
    this.worker = null;
    this.timer = 0;
    /** @type {(() => void)|null} */
    this.settle = null;
  }

  has(/** @type {string} */ name) { return !this.names || this.names.has(name); }

  /**
   * Makes pattern the filter. The returned promise ends when it is in effect,
   * or was refused (problem says why, and the last result stays).
   * @param {string} pattern already through cleanPattern
   * @returns {Promise<void>}
   */
  set(pattern) {
    this.cancel();
    if (!pattern) return this.done('', null, '');
    if (pattern === this.pattern && !this.problem) return Promise.resolve();
    try {
      new RegExp(pattern, 'i');
    } catch {
      return this.done(this.pattern, this.names, 'invalid');
    }
    const id = ++this.seq;
    const texts = this.rows.map((r) => r.fields);
    try {
      if (!this.worker) {
        this.worker = new Worker('static/match.js');
        this.worker.onmessage = (e) => this.answer(e.data);
        this.worker.onerror = () => this.stop('slow');
      }
      this.worker.postMessage({ id, pattern, rows: texts });
    } catch {
      // No worker here: the text is matched as it is, not as a pattern.
      const q = pattern.toLowerCase();
      return this.done(pattern, new Set(this.rows.filter((r) => r.fields.some((s) => s.toLowerCase().includes(q))).map((r) => r.name)), '');
    }
    this.want = pattern;
    this.timer = window.setTimeout(() => this.stop('slow'), LIMIT_MS);
    return new Promise((resolve) => { this.settle = resolve; });
  }

  /** @param {{id:number,hits?:number[],invalid?:boolean}} m */
  answer(m) {
    if (m.id !== this.seq || !this.settle) return;
    window.clearTimeout(this.timer);
    const settle = this.settle;
    this.settle = null;
    if (m.invalid || !m.hits) void this.done(this.pattern, this.names, 'invalid');
    else void this.done(/** @type {string} */ (this.want), new Set(m.hits.map((i) => this.rows[i].name)), '');
    settle();
  }

  /** Ends a match that is still running; the worker is replaced on the next one. */
  stop(/** @type {'slow'} */ why) {
    if (!this.settle) return;
    window.clearTimeout(this.timer);
    this.worker?.terminate();
    this.worker = null;
    const settle = this.settle;
    this.settle = null;
    void this.done(this.pattern, this.names, why);
    settle();
  }

  /** Drops a match in flight (a newer pattern replaces it), with its worker: it may be a slow one. */
  cancel() {
    if (!this.settle) return;
    window.clearTimeout(this.timer);
    this.worker?.terminate();
    this.worker = null;
    this.seq++;
    const settle = this.settle;
    this.settle = null;
    settle();
  }

  /** @param {string} pattern @param {Set<string>|null} names @param {''|'invalid'|'slow'} problem */
  done(pattern, names, problem) {
    this.pattern = pattern;
    this.names = names;
    this.problem = problem;
    this.onChange();
    return Promise.resolve();
  }
}
