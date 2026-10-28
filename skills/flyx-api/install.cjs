const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");

function linkTarget(target) {
  try {
    if (!fs.lstatSync(target).isSymbolicLink()) return null;
    return path.resolve(path.dirname(target), fs.readlinkSync(target));
  } catch (error) {
    if (error.code === "ENOENT") return undefined;
    throw error;
  }
}

function isSkillPackage(source) {
  if (!source) return false;
  try {
    const { name } = JSON.parse(fs.readFileSync(path.join(source, "package.json")));
    return name === "@flyx/skill-api" || name === "@flvx/skill-api";
  } catch {
    return false;
  }
}

function install(action, directory, source = __dirname) {
  const target = path.join(directory, "flyx-api");
  const current = linkTarget(target);
  if (action === "unlink") {
    if (current === source) fs.unlinkSync(target);
    return;
  }
  if (action !== "link") throw new Error("Expected link or unlink");
  if (current !== undefined && current !== source && !isSkillPackage(current)) {
    throw new Error(`Preserving existing file, directory or unrelated link: ${target}`);
  }
  fs.mkdirSync(directory, { recursive: true });
  if (current !== source) {
    if (current !== undefined) fs.unlinkSync(target);
    fs.symlinkSync(source, target, "dir");
  }
  const legacy = path.join(directory, "flvx-api");
  const previous = linkTarget(legacy);
  if (previous === source || isSkillPackage(previous)) fs.unlinkSync(legacy);
}

if (require.main === module) {
  try {
    install(process.argv[2], path.join(os.homedir(), ".agents", "skills"));
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}

module.exports = { install };
