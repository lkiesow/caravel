# MapLibre GL JS 6.6.0 (vendored)

Like `../leaflet/` before it, this is committed by hand rather than pulled by a
build step -- the app ships hand-written ES modules served straight from `web/`
with no bundler and no runtime npm dependencies (see `package.json`).

Licence: 3-Clause BSD, `LICENSE.txt`, retained as the licence requires. The
entry file carries the same notice in its header.

## Re-vendoring

```sh
curl -sSL -o maplibre.tgz https://registry.npmjs.org/maplibre-gl/-/maplibre-gl-6.6.0.tgz
tar xzf maplibre.tgz \
  package/dist/maplibre-gl.mjs \
  package/dist/maplibre-gl-shared.mjs \
  package/dist/maplibre-gl-worker.mjs \
  package/dist/maplibre-gl.css \
  package/LICENSE.txt
```

The files are byte-identical to the published tarball -- nothing is rewritten,
including the trailing `sourceMappingURL` comments, which 404 because the
`.map` files are not vendored. That is the same trade `leaflet.esm.js` already
makes. To verify a copy:

```
d84cb65fa75f07a972616cb4ee1902829ca053beae28f7be9b0889131c497afd  maplibre-gl.mjs
34c2cb0330cec92e81c4fa7344e7008451442bbb9cca1da3465db4041a934073  maplibre-gl-shared.mjs
b081c9b3d0691d9d85552b5624f2601f69f24ed37573959d279d322e98e4ee2f  maplibre-gl-worker.mjs
8e2dbbab312dc57656fbb76e9fa5308c75c9d7c7ba5808a7d55bcdb64cc813fa  maplibre-gl.css
```

## Three files, and the filenames are load-bearing

v6 is ESM-only and split across three modules. Do not rename them:

- `maplibre-gl.mjs` -- the entry, the only file the app imports.
- `maplibre-gl-shared.mjs` -- imported by the entry as the literal string
  `"./maplibre-gl-shared.mjs"`.
- `maplibre-gl-worker.mjs` -- never imported. The entry *derives* its URL at
  runtime from its own `import.meta.url`:
  `new URL("./maplibre-gl-worker.mjs", import.meta.url)`, choosing the
  `-dev.mjs` variant when its own name ends that way. So the worker must sit
  in this directory under exactly this name, and the entry must not be renamed
  to anything ending in `-dev.mjs`.

`maplibregl.setWorkerUrl()` is the escape hatch if the worker ever has to live
elsewhere.

## Notes for whoever touches this next

- **`.mjs` is a served extension now.** `web/sw.js`'s `isCodeRequest()` had to
  learn about it; without that these files would fall into
  `staleWhileRevalidate` and a deploy would take two reloads to pick up.
- **`scripts/check_js.sh` does not see these files** -- it walks
  `web/js -name '*.js'`. That is accepted rather than worked around: the files
  are unmodified upstream output, and the checksums above are stronger
  evidence than a syntax parse. (`leaflet.esm.js` *was* covered, incidentally,
  by virtue of its extension.)
- **No image assets.** Every `url()` in `maplibre-gl.css` is an inline data
  URI, so there is no `images/` directory to vendor and none of the
  `leaflet/images/` trouble.
- **WebGL2 is required** -- v6 dropped the WebGL1 fallback.
