#!/usr/bin/env node

const https = require("https");
const fs = require("fs");
const path = require("path");
const { execFileSync } = require("child_process");
const os = require("os");

const REPO_OWNER = "supermodeltools";
const REPO_NAME = "Uncompact";
const BINARY_NAME = "uncompact";

function getPackageVersion() {
  const pkgPath = path.join(__dirname, "..", "package.json");
  const pkg = JSON.parse(fs.readFileSync(pkgPath, "utf8"));
  return pkg.version;
}

function getPlatform() {
  const platform = os.platform();
  if (platform === "darwin") return "darwin";
  if (platform === "linux") return "linux";
  if (platform === "win32") return "windows";
  throw new Error(`Unsupported platform: ${platform}`);
}

function getArch() {
  const arch = os.arch();
  if (arch === "x64") return "amd64";
  if (arch === "arm64") return "arm64";
  throw new Error(`Unsupported architecture: ${arch}`);
}

function getBinaryName(platform) {
  return platform === "windows" ? `${BINARY_NAME}.exe` : BINARY_NAME;
}

function getAssetName(platform, arch) {
  const ext = platform === "windows" ? ".zip" : ".tar.gz";
  return `${BINARY_NAME}_${platform}_${arch}${ext}`;
}

function httpsGet(url) {
  return new Promise((resolve, reject) => {
    const request = https.get(url, { headers: { "User-Agent": "uncompact-npm" } }, (response) => {
      if (response.statusCode >= 300 && response.statusCode < 400 && response.headers.location) {
        httpsGet(response.headers.location).then(resolve).catch(reject);
        return;
      }
      if (response.statusCode !== 200) {
        reject(new Error(`HTTP ${response.statusCode}: ${url}`));
        return;
      }
      const chunks = [];
      response.on("data", (chunk) => chunks.push(chunk));
      response.on("end", () => resolve(Buffer.concat(chunks)));
      response.on("error", reject);
    });
    request.setTimeout(20000, () => {
      request.destroy();
      reject(new Error(`Request timed out: ${url}`));
    });
    request.on("error", reject);
  });
}

function httpsGetJson(url) {
  return httpsGet(url).then((buffer) => JSON.parse(buffer.toString()));
}

async function getRelease(version) {
  if (version && version !== "0.0.0") {
    const tag = version.startsWith("v") ? version : `v${version}`;
    const url = `https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases/tags/${tag}`;
    return await httpsGetJson(url);
  }
  const url = `https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases/latest`;
  return httpsGetJson(url);
}

function extractTarGz(buffer, destDir, binaryName) {
  const tarPath = path.join(destDir, "archive.tar.gz");
  fs.writeFileSync(tarPath, buffer);
  execFileSync("tar", ["-xzf", tarPath, "-C", destDir], { stdio: "pipe" });
  fs.unlinkSync(tarPath);
  
  const extracted = path.join(destDir, binaryName);
  if (!fs.existsSync(extracted)) {
    throw new Error(`Binary not found after extraction: ${extracted}`);
  }
  return extracted;
}

function extractZip(buffer, destDir, binaryName) {
  const zipPath = path.join(destDir, "archive.zip");
  fs.writeFileSync(zipPath, buffer);
  
  if (process.platform === "win32") {
    execFileSync("powershell", ["-Command", `Expand-Archive -Path '${zipPath}' -DestinationPath '${destDir}' -Force`], { stdio: "pipe" });
  } else {
    execFileSync("unzip", ["-o", zipPath, "-d", destDir], { stdio: "pipe" });
  }
  fs.unlinkSync(zipPath);
  
  const extracted = path.join(destDir, binaryName);
  if (!fs.existsSync(extracted)) {
    throw new Error(`Binary not found after extraction: ${extracted}`);
  }
  return extracted;
}

function log(msg) {
  process.stderr.write(msg);
  // Only write to /dev/tty if stderr is NOT a TTY, to avoid double-logging
  if (!process.stderr.isTTY && process.platform !== "win32") {
    try {
      const tty = fs.openSync("/dev/tty", "w");
      fs.writeSync(tty, msg);
      fs.closeSync(tty);
    } catch (err) {
      // Ignore TTY errors
    }
  }
}

async function main() {
  const platform = getPlatform();
  const arch = getArch();
  const assetName = getAssetName(platform, arch);
  const binaryName = getBinaryName(platform);
  const binDir = path.join(__dirname, "bin");

  log(`[uncompact] Post-install setup for ${platform}/${arch}...\n`);

  if (!fs.existsSync(binDir)) {
    fs.mkdirSync(binDir, { recursive: true });
  }

  const destPath = path.join(binDir, binaryName);

  if (!fs.existsSync(destPath)) {
    const version = getPackageVersion();
    let release;
    try {
      release = await getRelease(version);
    } catch (err) {
      log(`[uncompact] Failed to fetch release: ${err.message}\n`);
      log(`[uncompact] You can install manually: go install github.com/${REPO_OWNER}/${REPO_NAME.toLowerCase()}@latest\n`);
      process.exit(0);
    }

    const asset = release.assets.find((a) => a.name === assetName);
    if (!asset) {
      log(`[uncompact] No binary found for ${platform}/${arch} in release ${release.tag_name}\n`);
      log(`[uncompact] Available assets: ${release.assets.map((a) => a.name).join(", ")}\n`);
      log(`[uncompact] You can install manually: go install github.com/${REPO_OWNER}/${REPO_NAME.toLowerCase()}@latest\n`);
      process.exit(0);
    }

    log(`[uncompact] Downloading ${BINARY_NAME} ${release.tag_name}...\n`);

    let buffer;
    try {
      buffer = await httpsGet(asset.browser_download_url);
    } catch (err) {
      log(`[uncompact] Failed to download: ${err.message}\n`);
      process.exit(0);
    }

    log(`[uncompact] Extracting...\n`);

    try {
      if (platform === "windows") {
        extractZip(buffer, binDir, binaryName);
      } else {
        extractTarGz(buffer, binDir, binaryName);
      }
    } catch (err) {
      log(`[uncompact] Failed to extract: ${err.message}\n`);
      process.exit(0);
    }

    if (platform !== "windows") {
      fs.chmodSync(destPath, 0o755);
    }

    log(`[uncompact] Installed to ${destPath}\n\n`);
  }

  // Automatically install Claude Code hooks
  log("[uncompact] Configuring Claude Code hooks...\n");
  try {
    execFileSync(destPath, ["install", "--yes"], { stdio: "inherit" });
  } catch (err) {
    log("[uncompact] Note: Automatic hook configuration skipped or failed. Run manually if needed:\n");
    log("  uncompact install\n");
  }

  // Show status to verify setup
  try {
    console.log();
    execFileSync(destPath, ["status"], { stdio: "inherit" });
  } catch (err) {
    try {
      execFileSync(destPath, [], { stdio: "inherit" });
    } catch (e) {}
  }

  log("\n");
  log("[uncompact] To authenticate: run 'uncompact auth login'\n");
}

main().catch((err) => {
  console.error(`[uncompact] Installation failed: ${err.message}`);
  process.exit(0);
});
