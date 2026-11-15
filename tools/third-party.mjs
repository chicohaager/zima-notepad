// Writes the licences of every npm package that ends up in editor.js, read
// from esbuild's metafile, to stdout: name, version, licence field, full text.
// Usage: node tools/third-party.mjs build/editor-meta.json > THIRD-PARTY.txt
import fs from 'node:fs';
import path from 'node:path';

const meta = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
const webDir = path.resolve(path.dirname(process.argv[2]), '..', 'web');
const pkgs = new Map();
for (const input of Object.keys(meta.inputs)) {
  const m = input.match(/node_modules\/((?:@[^/]+\/)?[^/]+)\//);
  if (m) pkgs.set(m[1], path.join(webDir, 'node_modules', m[1]));
}
if (pkgs.size === 0) { console.error('no packages in the metafile'); process.exit(1); }
const out = ['Third-party software bundled in /usr/share/casaos/www/modules/notepad/editor.js', ''];
for (const [name, dir] of [...pkgs].sort()) {
  const pj = JSON.parse(fs.readFileSync(path.join(dir, 'package.json'), 'utf8'));
  const lf = fs.readdirSync(dir).find((f) => /^(licen[cs]e|copying)/i.test(f));
  if (!lf) { console.error(`${name}: no licence file`); process.exit(1); }
  out.push('='.repeat(72), `${name} ${pj.version} (${pj.license})`, '-'.repeat(72), fs.readFileSync(path.join(dir, lf), 'utf8').trim(), '');
}
process.stdout.write(out.join('\n') + '\n');
console.error(`${pkgs.size} packages`);
