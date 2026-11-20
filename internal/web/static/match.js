// fyisp dashboard: the filter's pattern matching, in a worker of its own. A
// regular expression can take exponential time on the wrong pattern, and the
// pattern can arrive in a link: here the page can stop it (filter.js).
// @ts-check

self.onmessage = (/** @type {MessageEvent<{id:number,pattern:string,rows:string[][]}>} */ e) => {
  const { id, pattern, rows } = e.data;
  let re;
  try {
    re = new RegExp(pattern, 'i');
  } catch {
    self.postMessage({ id, invalid: true });
    return;
  }
  /** @type {number[]} */
  const hits = [];
  for (let i = 0; i < rows.length; i++) if (rows[i].some((s) => re.test(s))) hits.push(i);
  self.postMessage({ id, hits });
};
