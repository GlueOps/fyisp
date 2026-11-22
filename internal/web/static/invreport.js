// Investigate's reports in mtr's formats (ui/report.c in
// github.com/traviscross/mtr): `mtr -rwz` text, `mtr -j` JSON and `mtr -C`
// CSV, built in the browser from /api/trace. The source host is always
// "fyisp": never this machine's name or the user's address.
// @ts-check

/**
 * One hop row of /api/trace (the fields used here).
 * @typedef {{hop:number,ip?:string,no_reply?:boolean,private?:boolean,rdns?:string,asn?:number,
 *   n:number,lost:number,loss:number,min:number|null,mean:number|null,max:number|null,
 *   last?:number|null,stdev?:number|null}} ReportHop
 */
/**
 * @typedef {{dst:string,start:number,end:number,intervalMs:number,hops:ReportHop[],mode?:string,port?:number,viaICMP?:boolean,destHop?:number}} ReportInput
 */

export const SRC = 'fyisp';
// Probe sizes (internal/netinfo/hops), IPv4 header included. ICMP echo: 8
// bytes of ICMP header and 16 of payload. TCP: a SYN with the usual
// options. UDP: 8 bytes of header and 8 of payload.
const PSIZE = { icmp: '44', tcp: '60', udp: '36' };
// internal/trace.DefaultRouterRate: probes answered per router per second.
const ROUTER_RATE = 1;

/** One line per hop number (the first row of each, the one with most probes), with every TTL up to the last. @param {ReportHop[]} hops */
function byHop(hops) {
  /** @type {Map<number, ReportHop[]>} */
  const m = new Map();
  for (const x of hops) {
    if (!m.has(x.hop)) m.set(x.hop, []);
    /** @type {ReportHop[]} */ (m.get(x.hop)).push(x);
  }
  const last = hops.reduce((a, x) => Math.max(a, x.hop), 0);
  /** @type {{hop:number,main:ReportHop|null,more:ReportHop[]}[]} */
  const out = [];
  for (let n = 1; n <= last; n++) {
    const rows = m.get(n) || [];
    out.push({ hop: n, main: rows[0] || null, more: rows.slice(1) });
  }
  return out;
}

/** mtr's host name with -b: "name (ip)", the address alone, or ??? for a silent hop. @param {ReportHop|null} x */
export function hostName(x) {
  if (!x || x.no_reply || !x.ip) return '???';
  return x.rdns ? `${x.rdns} (${x.ip})` : x.ip;
}

/** @param {ReportHop|null} x */
export function asnName(x) { return x && x.asn ? `AS${x.asn}` : 'AS???'; }

/** mtr's numbers for one hop: RTTs are 0 without answers (as mtr's); Last and StDev null when not known. @param {ReportHop|null} x */
function nums(x) {
  const snt = x ? x.n + x.lost : 0;
  const has = !!x && x.n > 0;
  const v = (/** @type {number|null|undefined} */ f) => (has && f != null && isFinite(f) ? f : has ? null : 0);
  return {
    loss: snt ? (x ? x.lost : 0) / snt * 100 : 0, snt,
    last: v(x && x.last), avg: v(x && x.mean), best: v(x && x.min), wrst: v(x && x.max), stdev: v(x && x.stdev),
  };
}

const r2 = (/** @type {number|null} */ f) => (f == null ? null : Math.round(f * 100) / 100);

/** mtr's report time: 2026-10-16T10:53:47+0000. @param {number} ms */
function isoTime(ms) { return new Date(ms).toISOString().replace(/\.\d+Z$/, '+0000'); }

// The probes' destination port when the trace doesn't say (TCP, UDP).
export const PORT = { tcp: 443, udp: 33434 };

/** The probe type, in words. @param {ReportInput} r */
export function probeName(r) {
  if (r.mode === 'tcp') return r.viaICMP ? `ICMP echo and TCP SYN (port ${r.port || PORT.tcp}) probes at each TTL, the routers named from the ICMP replies` : `TCP SYN probes to port ${r.port || PORT.tcp}`;
  if (r.mode === 'udp') return `UDP probes to port ${r.port || PORT.udp}`;
  return 'ICMP echo probes';
}

/** How fyisp measured, for the footers: the probes and the round interval, and the time range. @param {ReportInput} r */
const probes = (r) => `${probeName(r)}, one round every ${Math.round(r.intervalMs / 100) / 10} s`;
const span = (/** @type {ReportInput} */ r) => `${isoTime(r.start)} to ${isoTime(r.end)}`;

/** The footer of the mtr and ping reports. @param {ReportInput} r */
function footer(r) {
  return `fyisp: ${probes(r)}, at most ${ROUTER_RATE} probe a second per router (a busy router misses some rounds); ${span(r)}.`;
}

/**
 * The rows up to the destination (destHop, the current route's length): a deeper row can hold a few stray
 * samples (a lost probe at the destination's TTL in the first round, or an older, longer route).
 * @param {ReportInput} r
 */
function toDest(r) { return byHop(r.hops).filter((x) => !r.destHop || x.hop <= r.destHop); }

/** One hop's host as mtr -z prints it: "AS64500  name (ip)". @param {ReportHop|null} x */
export function mtrLabel(x) { return `${asnName(x)} `.padEnd(9) + hostName(x); }

/**
 * One hop's mtr numbers as mtr -rwz prints them: Loss% to 2 decimals, the times to 1 (0.0 without answers, as
 * mtr; - when not known). Investigate's table shows these same strings.
 * @param {ReportHop|null} x
 */
export function mtrCells(x) {
  const v = nums(x);
  const t = (/** @type {number|null} */ f) => (f == null ? '-' : f.toFixed(1));
  return { loss: v.loss.toFixed(2) + '%', snt: String(v.snt), last: t(v.last), avg: t(v.avg), best: t(v.best), wrst: t(v.wrst), stdev: t(v.stdev) };
}

/** `mtr -rwz` text. @param {ReportInput} r */
export function mtrText(r) {
  const rows = byHop(r.hops);
  const w = Math.max(SRC.length + 2, ...rows.map((x) => mtrLabel(x.main).length + 2)) + 1;
  const head = ('HOST: ' + SRC).padEnd(8 + w) + '   Loss%   Snt   Last   Avg  Best  Wrst StDev';
  const lines = [`Start: ${isoTime(r.start)}`, head];
  // A cell keeps a space before it however wide its value (e.g. 9999.9).
  const c = (/** @type {string} */ v) => (' ' + v).padStart(6);
  for (const { hop, main, more } of rows) {
    const v = mtrCells(main);
    lines.push(`${String(hop).padStart(3)}.|-- ${mtrLabel(main).padEnd(w)}` +
      `${v.loss.padStart(8)}${v.snt.padStart(6)} ${c(v.last)}${c(v.avg)}${c(v.best)}${c(v.wrst)}${c(v.stdev)}`);
    for (const m of more) lines.push(`        ${mtrLabel(m)}`);
  }
  lines.push('', footer(r));
  return lines.join('\n') + '\n';
}

/** `mtr -j` JSON (with -z: each hub's ASN). @param {ReportInput} r */
export function mtrJSON(r) {
  const rows = byHop(r.hops);
  const hubs = rows.map(({ hop, main }) => {
    const v = nums(main);
    return { count: hop, host: hostName(main), ASN: asnName(main), 'Loss%': r2(v.loss), Snt: v.snt,
      Last: r2(v.last), Avg: r2(v.avg), Best: r2(v.best), Wrst: r2(v.wrst), StDev: r2(v.stdev) };
  });
  const tests = rows.reduce((m, x) => Math.max(m, nums(x.main).snt), 0);
  const mode = r.mode || 'icmp';
  /** @type {Record<string, string|number>} */
  const mtr = { src: SRC, dst: r.dst, tos: 0, tests, psize: PSIZE[/** @type {'icmp'|'tcp'|'udp'} */ (mode)] || PSIZE.icmp, bitpattern: 'n/a' };
  // mtr -j has no probe type: added for TCP and UDP only, so ICMP reports stay mtr's.
  if (mode !== 'icmp') Object.assign(mtr, { type: mode, port: r.port || (mode === 'tcp' ? 443 : 33434), probe: probeName(r) });
  return JSON.stringify({ report: { mtr, hubs } }, null, 4) + '\n';
}

/** `mtr -C` CSV (with -z: the Asn column). The empty column is mtr's field separator. @param {ReportInput} r */
export function mtrCSV(r) {
  const q = (/** @type {string} */ s) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
  const f = (/** @type {number|null} */ v) => (v == null ? '' : v.toFixed(2));
  const lines = ['Mtr_Version,Start_Time,Status,Host,Hop,Ip,Asn,Loss%,Snt,,Last,Avg,Best,Wrst,StDev'];
  const t = Math.floor(r.start / 1000);
  for (const { hop, main } of byHop(r.hops)) {
    const v = nums(main);
    lines.push([SRC, t, 'OK', q(r.dst), hop, q(hostName(main)), asnName(main), f(v.loss), v.snt, '', f(v.last), f(v.avg), f(v.best), f(v.wrst), f(v.stdev)].join(','));
  }
  return lines.join('\n') + '\n';
}

/** Ping's statistics for the destination, as `ping -c N` prints them, from the same rounds. @param {ReportInput} r */
export function pingText(r) {
  const rows = toDest(r);
  const last = rows.length ? rows[rows.length - 1].main : null;
  const v = nums(last);
  const rx = last ? last.n : 0;
  const f3 = (/** @type {number|null} */ f) => (f == null ? '-' : f.toFixed(3));
  const lines = [`--- ${r.dst} ping statistics ---`,
    `${v.snt} packets transmitted, ${rx} received, ${+v.loss.toFixed(2)}% packet loss`];
  if (rx) lines.push(v.stdev == null ? `rtt min/avg/max = ${f3(v.best)}/${f3(v.avg)}/${f3(v.wrst)} ms` : `rtt min/avg/max/mdev = ${f3(v.best)}/${f3(v.avg)}/${f3(v.wrst)}/${f3(v.stdev)} ms`);
  lines.push('', `${footer(r)} The destination is hop ${last ? last.hop : 0}${rx ? '' : ', which never answered'}; mdev is the standard deviation.`);
  return lines.join('\n') + '\n';
}

/**
 * `traceroute` text: one line per hop up to the destination (destHop), "* * *" for a hop that never answered.
 * The three times are the hop's best, average and worst over the rounds, not three single probes; the footer
 * says so.
 * @param {ReportInput} r
 */
export function tracerouteText(r) {
  const rows = toDest(r);
  const dest = rows.length ? rows[rows.length - 1].main : null;
  const ip = dest && !dest.no_reply && dest.ip && dest.ip !== r.dst ? ` (${dest.ip})` : '';
  const mode = r.mode || 'icmp';
  const how = mode === 'tcp' ? `, TCP SYN to port ${r.port || PORT.tcp}` : mode === 'udp' ? `, UDP to port ${r.port || PORT.udp}` : '';
  const lines = [`traceroute to ${r.dst}${ip}, 30 hops max${how}`];
  const ms = (/** @type {number|null} */ f) => (f == null ? '*' : `${f.toFixed(3)} ms`);
  let snt = 0;
  for (const { hop, main } of rows) {
    snt = Math.max(snt, nums(main).snt);
    const n = String(hop).padStart(2);
    if (!main || main.n === 0 || !main.ip) { lines.push(`${n}  * * *`); continue; }
    lines.push(`${n}  ${hostName(main)}  ${ms(main.min)}  ${ms(main.mean)}  ${ms(main.max)}`);
  }
  lines.push('', `fyisp: the three times are best, average and worst over ${snt} rounds (not three single probes); ${probes(r)}; ${span(r)}.`);
  return lines.join('\n') + '\n';
}

/** The command to run the same trace yourself, with the same probes. @param {string} host @param {string} [mode] @param {number} [port] TCP's */
export function mtrCommand(host, mode = 'icmp', port = PORT.tcp) {
  const how = mode === 'tcp' ? `-T -P ${port} ` : mode === 'udp' ? `-u -P ${PORT.udp} ` : '';
  return `mtr ${how}-rwzbc 100 ${host}`;
}
/** @param {string} host */
export function pathpingCommand(host) { return `pathping -n ${host}`; }
