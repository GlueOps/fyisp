# Changelog

## [0.16.0](https://github.com/GlueOps/fyisp/compare/v0.15.0...v0.16.0) (2026-11-20)


### ⚠ BREAKING CHANGES

* the Go module path and the container image name changed.
* the Go module path and the container image name changed.
* **web:** require the admin token for the whole dashboard
* **speedtest:** more US iperf3 servers by default, drop InterServer, no ISP blame from speed tests
* drop the ot= filter and home: categories, legacy hash and speed probes, dead CSS
* drop the speed-test byte cap, its v0.7 compat and API fields no reader uses
* **store:** replace any database with an older schema by one rule
* **export:** CSV of raw samples only; drop --format sqlite and --tier 1h
* **web:** the admin token guards speed-test controls only; traces get an hourly start budget
* **store:** start over with one schema (v7) and no migrations
* drop the report and redact packages and report-only state
* **cli:** remove fyisp report
* **web:** remove the notes and reports routes
* **ui:** remove notes, reports and Mark a bad call from the dashboard
* **web:** serve the new Home at /api/home and delete the old one
* remove --share and the embedded cloudflared tunnel

### Features

* **cli:** remove fyisp report ([10a1550](https://github.com/GlueOps/fyisp/commit/10a15500805d0c979edf160374e20952fa0141ba))
* **deps:** update dependencies to their latest versions ([#136](https://github.com/GlueOps/fyisp/issues/136)) ([ead3b6a](https://github.com/GlueOps/fyisp/commit/ead3b6ab63f7fbcbbc39301fedef972dfa48ac8b))
* **export:** CSV of raw samples only; drop --format sqlite and --tier 1h ([af5aca2](https://github.com/GlueOps/fyisp/commit/af5aca20d6a572f62f03a31b773be68e0042fb52))
* **hops:** ttl-limited tcp and udp probes without privileges ([531f27f](https://github.com/GlueOps/fyisp/commit/531f27f390e5dd8c3ee9f871ecdb83dbd5428646))
* mtr-style investigate, tcp/udp traces, trace any host; additive migrations; scoped chart incidents ([d7b21a7](https://github.com/GlueOps/fyisp/commit/d7b21a7aac7518ff6bf3d0c116d6ec3683555bc4))
* **netinfo:** expose the uplink connection type on the network path ([a501e40](https://github.com/GlueOps/fyisp/commit/a501e404a17c3294ab96a709926c73385b10a290))
* **netinfo:** expose the uplink connection type on the network path ([a05cde6](https://github.com/GlueOps/fyisp/commit/a05cde6c7982896fb130eee573c524244474a0bf))
* **profile:** default covers every Home tile ([a5d801d](https://github.com/GlueOps/fyisp/commit/a5d801d66dd156a68890663ddef8e5172a81afd0))
* **profile:** default covers every Home tile (28 targets) ([09ee9b8](https://github.com/GlueOps/fyisp/commit/09ee9b84cf64b178c6cf3b427e58ecaccfc4e2ba))
* **profile:** drop Quad9 (9.9.9.9) from the network path and the catalog ([ec45b91](https://github.com/GlueOps/fyisp/commit/ec45b915cd4dd0c9ca995e1314743cb8ee113bc5))
* remove --share and the embedded cloudflared tunnel ([7d800b6](https://github.com/GlueOps/fyisp/commit/7d800b6e08904007329110319b1014849dfe9563))
* **report:** speed in the owner's time zone, one day count, slowest servers (B2, B3 server side) ([6984f45](https://github.com/GlueOps/fyisp/commit/6984f457e100be3522f89e8df712854bf9210958))
* **s3test:** a vendor-agnostic engine for storage test rounds ([ff5b9e8](https://github.com/GlueOps/fyisp/commit/ff5b9e8bea04803321938d1798451bc07233538a))
* **speed:** B2/B3 fields as the UI reads them, owner zone on the share link, bytes_per_test, cancelled_by ([3d9e5e4](https://github.com/GlueOps/fyisp/commit/3d9e5e45e1e55fc1d7c5b4f42dffb2bd155e5d5e))
* **speed:** cancelled_by (owner), slowest server and n, share-link owner zone test ([8d840b5](https://github.com/GlueOps/fyisp/commit/8d840b5246d07ea7f03ef1f66c5b37293358fa7d))
* **speedtest:** a 10 s time cap replaces the 100 MB byte cap (v0.7.2) ([6f29066](https://github.com/GlueOps/fyisp/commit/6f29066165d70875dfc40d5453f23e02747b8f81))
* **speedtest:** more US iperf3 servers by default, drop InterServer, no ISP blame from speed tests ([2621334](https://github.com/GlueOps/fyisp/commit/2621334f8a3955f500a5add46e8edc7e51694d32))
* **speedtest:** one steady-rate estimator for capped directions (A2) ([25d042c](https://github.com/GlueOps/fyisp/commit/25d042c5157c13de106a90550de88dfe9e03e67d))
* **speedtest:** pause the speed-test schedule ([bc7943e](https://github.com/GlueOps/fyisp/commit/bc7943e0cbf6fa391759c9574b8ba8401bbd1435))
* **speedtest:** time-only share link; one estimator for every direction, plateau-aware early stop (v0.7.2) ([62f6a4a](https://github.com/GlueOps/fyisp/commit/62f6a4ac21e448fcdd292113d1f23e2364409c36))
* storage tests (S3-compatible connection and throughput campaigns) ([0385a98](https://github.com/GlueOps/fyisp/commit/0385a983f50527506a1843a2e637e49006cb6003))
* **storage:** campaigns: rounds over time, stored attempt by attempt ([94edf34](https://github.com/GlueOps/fyisp/commit/94edf3485199a1d037ecaa89d26624f24af4b1db))
* **storage:** evidence: what a campaign points to, traceroutes, reports ([6fbd721](https://github.com/GlueOps/fyisp/commit/6fbd72176e726c97d3b59cf42216d99e85cab7dc))
* **storage:** storage-test targets and checks ([85f2fcf](https://github.com/GlueOps/fyisp/commit/85f2fcf28078d72d7c3ad69896e682badc7e9de6))
* **store:** hop kinds for tcp and udp traces ([62e6999](https://github.com/GlueOps/fyisp/commit/62e6999e1c6392c80211d9f670a395ef20b4774d))
* **store:** roll forward with additive migrations from the last release ([ba0537b](https://github.com/GlueOps/fyisp/commit/ba0537bd6b11522b66e9661b0eb9397df518958c))
* **store:** start over with one schema (v7) and no migrations ([507ed4a](https://github.com/GlueOps/fyisp/commit/507ed4af5efd22f2a3f6c275ca9759281468f1c3))
* **tiles:** engine, replay and wiring ([971e77f](https://github.com/GlueOps/fyisp/commit/971e77fd187bb6235a6c11ac731f295a050a8ce0))
* **tiles:** engine, replay and wiring (PR 3) ([3999b0c](https://github.com/GlueOps/fyisp/commit/3999b0c0815413968f4f535ae4c3c30ef532ca9a))
* **tiles:** fixed Home tile definitions, tile: tags and a OneDrive target ([47fd455](https://github.com/GlueOps/fyisp/commit/47fd4555a138e648382cbc2de328512815c212e1))
* **tiles:** fixed Home tile definitions, tile: tags and a OneDrive target ([08d6912](https://github.com/GlueOps/fyisp/commit/08d6912b55a5c565fd7a9869cd51e26cbaf4fb2e))
* **tiles:** partial loss makes a web or stream minute Slow ([cdea595](https://github.com/GlueOps/fyisp/commit/cdea5954c9a342ff1c4290a87ab5ed060b339ff0))
* **tiles:** tile status rules (pure functions) ([72ef8c3](https://github.com/GlueOps/fyisp/commit/72ef8c384b9a4951e44ba35fac8128fb18e08030))
* **tiles:** tile status rules as pure functions ([800e9df](https://github.com/GlueOps/fyisp/commit/800e9df9362b086141a39ca6798aafa84f6c4f62))
* **trace:** stop a round after 5 silent hops past the last answer ([bef4a96](https://github.com/GlueOps/fyisp/commit/bef4a96ce0056091588aa2000a26b20324541f0d))
* **trace:** tcp and udp modes for on-demand traces ([de07dd8](https://github.com/GlueOps/fyisp/commit/de07dd8b94aee8e5248400fe667865ea1d175f33))
* **trace:** trace hosts outside the profile as temporary targets ([2b8258a](https://github.com/GlueOps/fyisp/commit/2b8258a9f40b9c2f6f4c296d99c761653483636a))
* **tunnel:** --tunnel publishes the dashboard through a Cloudflare quick tunnel ([78d87ae](https://github.com/GlueOps/fyisp/commit/78d87ae6f4fd84a4a1ea6914157fc4c1b1633e6d))
* **ui:** a host box in investigate ([645327f](https://github.com/GlueOps/fyisp/commit/645327f3ae1efefa39ec5d9a4bd9b25459974120))
* **ui:** count and mark one target's probes over a threshold ([5d612c8](https://github.com/GlueOps/fyisp/commit/5d612c83bcb3639594d3c15cd45eb17eb90e08eb))
* **ui:** count and mark one target's probes over a threshold ([b3ac1c5](https://github.com/GlueOps/fyisp/commit/b3ac1c5ed3be41ef508ac63da52dd07aad4d1564))
* **ui:** icmp | tcp 443 | udp picker in investigate ([ed15126](https://github.com/GlueOps/fyisp/commit/ed151261956f456d2705a7841345c7cd42960849))
* **ui:** Investigate without the admin token; auto-start only for the owner ([9b79142](https://github.com/GlueOps/fyisp/commit/9b7914273c89877d48153b4d5f0acff49dc679eb))
* **ui:** mtr-style investigate table and report export ([0db1925](https://github.com/GlueOps/fyisp/commit/0db1925246de0e7d00dc192b729f061cf4b86277))
* **ui:** new Home (v0.9.0) behind the /api/home2 probe ([f65d190](https://github.com/GlueOps/fyisp/commit/f65d190aba634d8a525e59468957c858aacdbf88))
* **ui:** new Home (v0.9.0) behind the /api/home2 probe ([8d49a16](https://github.com/GlueOps/fyisp/commit/8d49a16bf083bdff6ca522c1ac0b0a361190119d))
* **ui:** remove notes, reports and Mark a bad call from the dashboard ([b2f9515](https://github.com/GlueOps/fyisp/commit/b2f9515468c52f75f4388e10a2624556f34c6e82))
* **ui:** storage campaigns on the Storage page ([16c9c5c](https://github.com/GlueOps/fyisp/commit/16c9c5c17dd6b39ff65ec63303ea19737fb9be22))
* **ui:** the Storage page: targets and checks ([7a216d1](https://github.com/GlueOps/fyisp/commit/7a216d17d93ba584ddedce7ddbd74feafb6f5228))
* **web:** /api/home2, the new Home's API (v0.9.0 PR 4) ([fbae7f5](https://github.com/GlueOps/fyisp/commit/fbae7f5615818491833ef688b5fc03e0131cd528))
* **web:** /api/home2, the new Home's API (v0.9.0 PR 4) ([83ce3d8](https://github.com/GlueOps/fyisp/commit/83ce3d80d78d035ab6547a2c0fe5c9e8920c9325))
* **web:** GET /api/probes lists one series' probes over a threshold ([27ba292](https://github.com/GlueOps/fyisp/commit/27ba29266189bcef43e240666fb552edbe1b8dbd))
* **web:** GET /api/probes lists one series' probes over a threshold ([7543fce](https://github.com/GlueOps/fyisp/commit/7543fce6b459aeca5b7ce33b0504129a6e197a43))
* **web:** investigate any public host ([ffe9359](https://github.com/GlueOps/fyisp/commit/ffe9359588a6f48b6e16669031d832bfff04bf28))
* **web:** remove the notes and reports routes ([7a94759](https://github.com/GlueOps/fyisp/commit/7a94759106ec4a1f1813a1e8a041d8de86475c99))
* **web:** require the admin token for the whole dashboard ([39a1025](https://github.com/GlueOps/fyisp/commit/39a102552617bd4f184306767034a5c944b09aa3))
* **web:** serve the new Home at /api/home and delete the old one ([438105a](https://github.com/GlueOps/fyisp/commit/438105a2b349cb65528f7613a688324a2c73a04a))
* **web:** speed-test marks are a legend toggle, off by default ([cd810a5](https://github.com/GlueOps/fyisp/commit/cd810a5068f1bcc0c16c5fefeb2bdd7976ca1def))
* **web:** the admin token guards speed-test controls only; traces get an hourly start budget ([1d35fad](https://github.com/GlueOps/fyisp/commit/1d35fadc6b8b2f4ff2fc8370768ca7111597a97b))


### Bug Fixes

* **ci:** flaky timing tests ([#15](https://github.com/GlueOps/fyisp/issues/15)) ([dc50cbb](https://github.com/GlueOps/fyisp/commit/dc50cbb9a5974f2ef216dc8f7766d3c9b393f9ef))
* **ci:** flaky timing tests ([#15](https://github.com/GlueOps/fyisp/issues/15)) ([7671ee4](https://github.com/GlueOps/fyisp/commit/7671ee4b637404201e7765f48dcecef971b6cf2e))
* **ci:** review of [#22](https://github.com/GlueOps/fyisp/issues/22): 500 ms relay, catch-up byte cap, clampTotal test, steady stall guard ([68254f8](https://github.com/GlueOps/fyisp/commit/68254f86f9548fc9ac923405ebdcf6f4a57d8d9a))
* **hops:** stop waiting on a tcp probe before the syn retransmit ([66f6d29](https://github.com/GlueOps/fyisp/commit/66f6d299594e273b1f38ef9c7f5f65d35e675d0e))
* **image:** add /tmp to the runtime image so --ephemeral starts ([9a9fe4d](https://github.com/GlueOps/fyisp/commit/9a9fe4de30b64988db94ac5fbf5f5c8bf8f276f4))
* **image:** add /tmp to the runtime image so --ephemeral starts ([da5ec68](https://github.com/GlueOps/fyisp/commit/da5ec68800e17590eb0bc04720f5907b1c58bfee))
* **image:** make /data 0700 in the runtime image ([cee00de](https://github.com/GlueOps/fyisp/commit/cee00de5816d6620ddc7e4b8a6f716a550331da1))
* keep probe samples taken during speed tests instead of blanking them ([33a7baa](https://github.com/GlueOps/fyisp/commit/33a7baab434aea81f6625e8669d4f1d5cc0e2fa8))
* **netinfo:** keep Path.Conn raw and export InContainer ([a3c10de](https://github.com/GlueOps/fyisp/commit/a3c10dee280049c1d38aaf75eb99017fae880fe6))
* **netns:** run every scenario at once and fail the step on failure ([dbf5c7b](https://github.com/GlueOps/fyisp/commit/dbf5c7bf22d9a079db5afcd659ad58dc585a6262)), closes [#43](https://github.com/GlueOps/fyisp/issues/43)
* **netns:** seed fast-evening-cut's history as time-capped rows ([d857db3](https://github.com/GlueOps/fyisp/commit/d857db3b1fefb64938c79e537c8107190d8383ae))
* **netns:** seed fast-evening-cut's history as time-capped rows ([8429847](https://github.com/GlueOps/fyisp/commit/8429847899ad79c4112de8835755f772295d0775))
* open issues [#35](https://github.com/GlueOps/fyisp/issues/35) [#36](https://github.com/GlueOps/fyisp/issues/36) [#37](https://github.com/GlueOps/fyisp/issues/37) [#43](https://github.com/GlueOps/fyisp/issues/43) [#45](https://github.com/GlueOps/fyisp/issues/45) [#51](https://github.com/GlueOps/fyisp/issues/51); netns nightly runs again ([0435a1a](https://github.com/GlueOps/fyisp/commit/0435a1a2ded46b34afdc3de835b2bad6496aaaf8))
* **packaging:** drop ProcSubset=pid so the systemd unit finds the gateway ([87cf3ae](https://github.com/GlueOps/fyisp/commit/87cf3aed413085cabda413357251cc92e6723a65))
* **packaging:** drop ProcSubset=pid so the unit can read routes ([179cd00](https://github.com/GlueOps/fyisp/commit/179cd0037ebbfd0851b72eb90d62623fb5369953))
* **profiles:** show the hosts targets really probe and trace ([135fe99](https://github.com/GlueOps/fyisp/commit/135fe99da88a0577fb1bfd5ad43a93d4b046b9db))
* review of [#39](https://github.com/GlueOps/fyisp/issues/39) (Investigate banner, owner-only auto-start, pasted token) ([41de742](https://github.com/GlueOps/fyisp/commit/41de742c51c51af0d17c0fc54eebf5fc512ff93b))
* **s3test:** classify and time attempts the same on macOS and Windows ([cf02bbe](https://github.com/GlueOps/fyisp/commit/cf02bbe90112b8df2b36e600a176a2d3cff99a32))
* **s3test:** final review: seeding, request bounds, upload goodput ([07b447c](https://github.com/GlueOps/fyisp/commit/07b447cf15a43e19057af6be447eba63a621e41d))
* **speedtest:** a deaf server's test keeps its result; old scores out on byte-capped servers; SECURITY.md time-only ([518f85b](https://github.com/GlueOps/fyisp/commit/518f85bf672e00612f6536d6738b924c7f7d6fb4))
* **speedtest:** A2 coverage guard, too_fast finding, relaxed lower-bound rule ([a7b6435](https://github.com/GlueOps/fyisp/commit/a7b643576187832ff697196e4c40e55635c1d40c))
* **speedtest:** A2 re-verify musts; zone_label, owner zone on the share link, bytes_per_test ([dcbdf25](https://github.com/GlueOps/fyisp/commit/dcbdf2597ca51c894e6c1aa1592b517769b90227))
* **speedtest:** first test 2 min after turning on, traceroute after the save, look through exchanges (A5, A7, A6) ([a8fa9d4](https://github.com/GlueOps/fyisp/commit/a8fa9d4749f517226843b809f7a6e1c242c36c50))
* **speedtest:** handoff traceroute before the window (A7, plan item 14); networks by (peer, exchange) (A6) ([5d831eb](https://github.com/GlueOps/fyisp/commit/5d831eb67091aa695f8fc6a1532813feceb2eed6))
* **speedtest:** honest byte counts on every run (A3), prove 9 connections (A4) ([4f9e22b](https://github.com/GlueOps/fyisp/commit/4f9e22b29ea93b179b0ff6ee34d81e514a0f7a2b))
* **speedtest:** idle RTT from TCP_INFO min_rtt, else the connect time; ticks log via the scheduler's logger ([ab71634](https://github.com/GlueOps/fyisp/commit/ab716341c76d7934c790c2ba0feb3c832b1d48b1))
* **speedtest:** net-isp's lower-bound rule, checked against real shaped lines ([b61d5de](https://github.com/GlueOps/fyisp/commit/b61d5de49092c4bbfe37f8ce3bfcc24405b84327))
* **speedtest:** never lose a finished test to a late cancel ([440a045](https://github.com/GlueOps/fyisp/commit/440a045101c301921a29605bfe784bae56f30178))
* **speedtest:** RST backstop at the cap + 5% per download (A1, iperf side) ([241eb76](https://github.com/GlueOps/fyisp/commit/241eb76243542d00bf3aeed2415b9e60d980b647))
* **speedtest:** smooth over the idle RTT, not a bloated line's loaded srtt (net-isp) ([f6e792b](https://github.com/GlueOps/fyisp/commit/f6e792b42da051d0d67565ed9caec52a10540d2f))
* **speedtest:** the climb fake rises 1.5x per tick, catch-up covers a client tick ([a214a2c](https://github.com/GlueOps/fyisp/commit/a214a2c48624a22f526b630838fdda6f207772fc))
* **speedtest:** upload bytes without TCP_INFO count what arrived ([98f1e77](https://github.com/GlueOps/fyisp/commit/98f1e775d1bcb70a5f4082d3593e13a93ab71dbb))
* **speedtest:** with w = 1 the plateau test runs on two medians (net-isp's amendment) ([4c70f6a](https://github.com/GlueOps/fyisp/commit/4c70f6a97a5d6049d546d53e676d65d2625a7c89))
* **speedtest:** written-bytes uploads leave the first tick out of the window (net-isp) ([0ae99ae](https://github.com/GlueOps/fyisp/commit/0ae99aefe54f489cec3d23ba0058aa703ef4432b))
* **storage:** final review: a paused round, response limits, schema and docs ([eac4be4](https://github.com/GlueOps/fyisp/commit/eac4be412dc024b3ba103c599482b92e6d405980))
* **storage:** final review: findings, sleep, keys after a stop, startup, cache ([70a1fbd](https://github.com/GlueOps/fyisp/commit/70a1fbd9c7600f868485c9b31377c8c854d1c048))
* **store:** clamp Raw's lookups to the hours the table has ([1d05fa2](https://github.com/GlueOps/fyisp/commit/1d05fa284568b9a18998e20c7c90fdbd0aa02262))
* **store:** read Raw's hour bounds with one key seek each ([5dd0058](https://github.com/GlueOps/fyisp/commit/5dd00584e279bdd651b012701c26634a68ee350a))
* subcommands in the Docker image, "looks fine" copy, webdev verdicts, doc line (C1-C4) ([b312ffe](https://github.com/GlueOps/fyisp/commit/b312ffe29d04efcae3fd690ce796f8eacd0dd370))
* **tiles,web:** lossy-card boundary, short outages, Working again span, speed line ([16e570b](https://github.com/GlueOps/fyisp/commit/16e570b3acb6ff450083f2caf47d8b34e90e8856))
* **tiles:** a blip absorbs only the incident that is its lag ([36d6a5f](https://github.com/GlueOps/fyisp/commit/36d6a5f471605d62556dbeee75c53fc45c5fe971))
* **tiles:** a blip right after another blip is part of the same spell ([b35284d](https://github.com/GlueOps/fyisp/commit/b35284da2a75fe0943e1116946c0b19f2edbbc84))
* **tiles:** a lag needs the apps back; path outages only when tile-wide ([c1edd97](https://github.com/GlueOps/fyisp/commit/c1edd975bb5297eb5006880b5fdc3de26b637f3f))
* **tiles:** a short total-loss run inside heavy loss is not "Not answering" ([de0254b](https://github.com/GlueOps/fyisp/commit/de0254bc2d007677b13fb9248de4cd7ad36a3af7))
* **tiles:** a young isp incident reads the edge's last minute from the onset ([11c4983](https://github.com/GlueOps/fyisp/commit/11c49836d399ed5f2b4360b9129a4dd1dbaa471d))
* **tiles:** a young isp incident's edge loss counts from the loss's onset ([0b487bb](https://github.com/GlueOps/fyisp/commit/0b487bb5f1149d7f06caac3f04a765bdce1cfe5a))
* **tiles:** a young isp incident's edge loss counts with its upper bound ([e47b5f2](https://github.com/GlueOps/fyisp/commit/e47b5f20c1084e1b45bf2f9c1d6bc4c527a1876b))
* **tiles:** an ended calls problem reads its shifted span ([41166da](https://github.com/GlueOps/fyisp/commit/41166da075f5474be1759d98f2fa985c074c98a7))
* **tiles:** bound the drop lag, never hide a real outage ([#34](https://github.com/GlueOps/fyisp/issues/34) review) ([6dcddd1](https://github.com/GlueOps/fyisp/commit/6dcddd1c2fd5081d1e6c59b0cfb32f8090745bef))
* **tiles:** calls blame from the lossier window; bound the lossy-run excuse ([526f923](https://github.com/GlueOps/fyisp/commit/526f92376fa90459c4aaa2b6ce4d1d330f4fb653))
* **tiles:** Home bugs found by the netns lab (drop lag, blips, blocked calls, provider loss) ([c311d41](https://github.com/GlueOps/fyisp/commit/c311d41367798cdaf86e5830cb8e66f7b0975f93))
* **tiles:** keep an ongoing problem's provider cause while the edge loses some ([2e61c4a](https://github.com/GlueOps/fyisp/commit/2e61c4a7e6f8e20a5e3b5344d387d04d319b5699))
* **tiles:** only a blip the path was clean after counts on the card ([4f27135](https://github.com/GlueOps/fyisp/commit/4f271355237a96e4439e19f6270223ce123da1c0))
* **tiles:** read a Choppy calls problem's layer evidence where its loss was ([37b1201](https://github.com/GlueOps/fyisp/commit/37b12010747a135baec720b817f3230d72b40521))
* **tiles:** red at 40% loss from the start; calls back sooner after loss ([1562a80](https://github.com/GlueOps/fyisp/commit/1562a80d83ca1a80141640d9028b3ff5eedc3c9e))
* **tiles:** review fixes for tile definitions ([7310a7d](https://github.com/GlueOps/fyisp/commit/7310a7d91b8f8d999a5cd43dd2b71dd1771aad42))
* **tiles:** review fixes: no latency Down, two lossy bursts, beyond the provider ([96eaf94](https://github.com/GlueOps/fyisp/commit/96eaf9462f51fdfed4e6677fbb241fe5db426f3a))
* **tiles:** review fixes: run tracker, tie clears, card and attribution ([d64ab1f](https://github.com/GlueOps/fyisp/commit/d64ab1f25e15bfd11216162ce969a863dab23af3))
* **tiles:** second review: live/replay contract, path minutes, word ends ([2dfd040](https://github.com/GlueOps/fyisp/commit/2dfd04023b68fc54b428a40251d6658ec287abb4))
* **tiles:** the card's edge window starts at the incident ([6598e6c](https://github.com/GlueOps/fyisp/commit/6598e6c5f6d46857f325fdd18c67061a62ad0dca))
* **tiles:** the lossy card reads the isp incident's peak loss ([712ddb2](https://github.com/GlueOps/fyisp/commit/712ddb23468581dabb12f3f05d555da67673b9c8))
* **tiles:** the verdict's incident before it is read; loss right after a drop ([6d880a3](https://github.com/GlueOps/fyisp/commit/6d880a3788d16398f5a2623307fdb051f8405210))
* **tiles:** what the netns lab found on Home ([8731d4e](https://github.com/GlueOps/fyisp/commit/8731d4ec7083ae2b3db856075d45fab0b29fc312))
* **trace:** count the silent run from the destination's known ttl ([37306a5](https://github.com/GlueOps/fyisp/commit/37306a51baa076cd97888f9e75c56d97d4f71108))
* **trace:** stop labelling real loss as router rate-limiting ([70d0f40](https://github.com/GlueOps/fyisp/commit/70d0f40a39ebcc01e886385b95c1ee5ef36c8a0e))
* **tunnel,web:** really refuse private-network routing; sign-in survives restarts and proxies ([ba7730a](https://github.com/GlueOps/fyisp/commit/ba7730ab76ec1dc640040f1c691a5b95aea21a79))
* **ui:** final review: pause while testing, scoped cell colours, form locks ([260f21f](https://github.com/GlueOps/fyisp/commit/260f21f9cb3439a70d8ddbc12155b9d122b42865))
* **ui:** give up a hung part=now poll after 10 s ([59e43ec](https://github.com/GlueOps/fyisp/commit/59e43ecb262a31eafaf3d873ceee1105824f1df8))
* **ui:** Home review fixes (background tab, probe, chart, speed, a11y) ([63e7024](https://github.com/GlueOps/fyisp/commit/63e70249f83efebbb64f7ce2100631b935214e48))
* **ui:** investigate's host links, report columns, loss and tooltip wording ([909312f](https://github.com/GlueOps/fyisp/commit/909312f3a2151442ece62c52e355fe3c8e30c1c8))
* **ui:** load the charts when the browser's locale tag is invalid ([e4afa39](https://github.com/GlueOps/fyisp/commit/e4afa3989b5b033d2f511d27a6a3c5a897e5b6d0))
* **ui:** mark a service incident only on the charts it affects ([2852d72](https://github.com/GlueOps/fyisp/commit/2852d7248e18c051b43b3e73e3cce571f0f96f87))
* **ui:** one Mbit/s formatter; Mbit/s columns stay in Mbit/s from 1 Gbit/s ([5242314](https://github.com/GlueOps/fyisp/commit/524231451775974c47c78db970f4b0747c55daf2))
* **ui:** scale the chart y axis on the mean lines ([cfe4709](https://github.com/GlueOps/fyisp/commit/cfe47095f77b2ba901fedd99fdfefb04fe5d775e))
* **ui:** scale the chart y axis on the mean lines ([062cd86](https://github.com/GlueOps/fyisp/commit/062cd860e6eb375d9edc5833a5cf7f61a229006d))
* **ui:** storage review: focus, keys state, in-use targets and copy ([f93a8fe](https://github.com/GlueOps/fyisp/commit/f93a8fe103fb6c732600a6a0caec656c738f4237))
* **verdict:** excuse dead STUN servers per port; distinct own-server-down note ([256e5ae](https://github.com/GlueOps/fyisp/commit/256e5ae25585cfe12a00452825ec34bce7d192d0))
* **verdict:** no STUN block while a STUN port answers; review fixes ([b82144f](https://github.com/GlueOps/fyisp/commit/b82144f208b2002d30cb29de2efd7c87faab7f89))
* **web:** --allow-host with --admin-token=off disables controls and traces ([913cc1e](https://github.com/GlueOps/fyisp/commit/913cc1ea67d79ac5be8fc62ab1d95dc6ca133aff))
* **web:** accept host trace names up to the longest host name ([81f84b9](https://github.com/GlueOps/fyisp/commit/81f84b9fb9c5120b88b93b2801e33104836d9d44))
* **web:** Home reads a stun_blocked connection state as udp_blocked ([0da55ed](https://github.com/GlueOps/fyisp/commit/0da55ed36f209afc6222127b119888cf7afa05a1))
* **web:** no nested parentheses in the share link's paused sentence ([c126bc6](https://github.com/GlueOps/fyisp/commit/c126bc6b27501a5a43f1b6cc95f61f747eca40de))
* **web:** re-review of [#28](https://github.com/GlueOps/fyisp/issues/28): blocked keeps app outages, restart, strip ([5365122](https://github.com/GlueOps/fyisp/commit/536512215510101b256081a439739abb0d6058fe))
* **web:** report no trace modes when the tracer is not running ([3613526](https://github.com/GlueOps/fyisp/commit/3613526fccf06586e66d96555cbf61e385c17d8b))
* **web:** review of [#28](https://github.com/GlueOps/fyisp/issues/28): honest cards, blocked calls, stable keys, startup ([11689a9](https://github.com/GlueOps/fyisp/commit/11689a9f78bea931f583e96aa384ed938dac89a7))
* **web:** serve only the bare host for "run it yourself" ([bd5d660](https://github.com/GlueOps/fyisp/commit/bd5d660b6db5edad043b7f21fecf780f28f3c2ac))
* **web:** sign-in after a restart, safer session cookie, token kept out of usage and pprof ([9d96ea3](https://github.com/GlueOps/fyisp/commit/9d96ea300c7dac689ea771b2dc1d89924e977ab9))
* **web:** the share link never tells tests apart (B5 API) ([ad35a54](https://github.com/GlueOps/fyisp/commit/ad35a540dfd3fce2f2f1564baa0380136a3ed6c4))


### Performance Improvements

* **store:** Raw looks up a series' hours by key ([48461c2](https://github.com/GlueOps/fyisp/commit/48461c2c41dddde6e23fc56c45a21fe7c0b10ff4))


### Miscellaneous Chores

* move to the GlueOps org and relicense under Apache-2.0 ([#80](https://github.com/GlueOps/fyisp/issues/80)) ([246f92e](https://github.com/GlueOps/fyisp/commit/246f92e837b6543e0e2a8b2b207c17c11c8843ed))


### Code Refactoring

* drop the ot= filter and home: categories, legacy hash and speed probes, dead CSS ([79d2a91](https://github.com/GlueOps/fyisp/commit/79d2a91f50da2aa3bf984132f9af5e202fba00d1))
* drop the report and redact packages and report-only state ([50310af](https://github.com/GlueOps/fyisp/commit/50310afa049b050736d570233e38e1ef8142f4db))
* drop the speed-test byte cap, its v0.7 compat and API fields no reader uses ([856c605](https://github.com/GlueOps/fyisp/commit/856c605bb5635a8eff46076b30069e981afc5767))
* **store:** replace any database with an older schema by one rule ([43d30ab](https://github.com/GlueOps/fyisp/commit/43d30abb7d3cc5875531ff0c705f4b891ffc0917))


### Continuous Integration

* cut releases with release-please ([#90](https://github.com/GlueOps/fyisp/issues/90)) ([0d5190a](https://github.com/GlueOps/fyisp/commit/0d5190a515b115cd221f4cc2c36c441bd414ddd4))

## [0.15.0](https://github.com/GlueOps/fyisp/compare/v0.14.0...v0.15.0) (2026-11-20)


### ⚠ BREAKING CHANGES

* the Go module path and the container image name changed.
* the Go module path and the container image name changed.

### Features

* **deps:** update dependencies to their latest versions ([#136](https://github.com/GlueOps/fyisp/issues/136)) ([ead3b6a](https://github.com/GlueOps/fyisp/commit/ead3b6ab63f7fbcbbc39301fedef972dfa48ac8b))


### Miscellaneous Chores

* move to the GlueOps org and relicense under Apache-2.0 ([#80](https://github.com/GlueOps/fyisp/issues/80)) ([246f92e](https://github.com/GlueOps/fyisp/commit/246f92e837b6543e0e2a8b2b207c17c11c8843ed))


### Continuous Integration

* cut releases with release-please ([#90](https://github.com/GlueOps/fyisp/issues/90)) ([0d5190a](https://github.com/GlueOps/fyisp/commit/0d5190a515b115cd221f4cc2c36c441bd414ddd4))
