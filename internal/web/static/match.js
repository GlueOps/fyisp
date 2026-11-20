// fyisp dashboard: the filter's pattern matching, in a worker of its own. A
// regular expression can take exponential time on the wrong pattern, and the
// pattern can arrive in a link: here the page can stop it (filter.js).
// @ts-check

self.onmessage = (/** @type {MessageEvent<{pattern:string,rows:string[][]}>} */ e) => {
  const re = new RegExp(e.data.pattern, 'i'); // the page has compiled it already
  /** @type {number[]} */
  const hits = [];
  e.data.rows.forEach((fields, i) => { if (fields.some((s) => re.test(s))) hits.push(i); });
  self.postMessage({ hits });
};
// The page starts timing a match only once this has loaded.
self.postMessage({ ready: true });
