const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { install } = require("./install.cjs");

function fixture(t) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "flyx-skill-"));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  return root;
}

test("fresh installation, repeated linking and uninstall", (t) => {
  const root = fixture(t);
  install("link", root);
  install("link", root);
  assert.equal(fs.realpathSync(path.join(root, "flyx-api")), __dirname);
  install("unlink", root);
  assert.equal(fs.existsSync(path.join(root, "flyx-api")), false);
});

test("migrate a recognized old package without loading the skill twice", (t) => {
  const root = fixture(t);
  const old = path.join(root, "old-package");
  fs.mkdirSync(old);
  fs.writeFileSync(path.join(old, "package.json"), '{"name":"@flvx/skill-api"}');
  fs.symlinkSync(old, path.join(root, "flvx-api"));
  install("link", root);
  assert.equal(fs.existsSync(path.join(root, "flvx-api")), false);
  assert.equal(fs.realpathSync(path.join(root, "flyx-api")), __dirname);
});

test("preserve directories and unrelated links", (t) => {
  const root = fixture(t);
  const target = path.join(root, "flyx-api");
  fs.mkdirSync(target);
  assert.throws(() => install("link", root), /Preserving/);
  install("unlink", root);
  assert.equal(fs.statSync(target).isDirectory(), true);
  fs.rmdirSync(target);
  fs.symlinkSync("some-other-skill", target);
  assert.throws(() => install("link", root), /Preserving/);
  assert.equal(fs.readlinkSync(target), "some-other-skill");
});
