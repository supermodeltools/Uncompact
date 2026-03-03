#!/usr/bin/env node

const https = require("https");
const fs = require("fs");
const path = require("path");
const { execSync } = require("child_process");
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
    try {
      return await httpsGetJson(url);
    } catch (err) {
      console.log(`[uncompact] Release ${tag} not found, falling back to latest`);
    }
  }
  const url = `https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases/latest`;
  return httpsGetJson(url);
}

function extractTarGz(buffer, destDir, binaryName) {
  const tarPath = path.join(destDir, "archive.tar.gz");
  fs.writeFileSync(tarPath, buffer);
  execSync(`tar -xzf "${tarPath}" -C "${destDir}"`, { stdio: "pipe" });
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
    execSync(`powershell -Command "Expand-Archive -Path '${zipPath}' -DestinationPath '${destDir}' -Force"`, { stdio: "pipe" });
  } else {
    execSync(`unzip -o "${zipPath}" -d "${destDir}"`, { stdio: "pipe" });
  }
  fs.unlinkSync(zipPath);
  
  const extracted = path.join(destDir, binaryName);
  if (!fs.existsSync(extracted)) {
    throw new Error(`Binary not found after extraction: ${extracted}`);
  }
  return extracted;
}

async function main() {
  const platform = getPlatform();
  const arch = getArch();
  const assetName = getAssetName(platform, arch);
  const binaryName = getBinaryName(platform);
  const binDir = path.join(__dirname, "bin");

  console.log(`[uncompact] Installing ${BINARY_NAME} for ${platform}/${arch}...`);

  if (!fs.existsSync(binDir)) {
    fs.mkdirSync(binDir, { recursive: true });
  }

  const destPath = path.join(binDir, binaryName);

  if (fs.existsSync(destPath)) {
    console.log(`[uncompact] Binary already exists at ${destPath}`);
    return;
  }

  const version = getPackageVersion();
  let release;
  try {
    release = await getRelease(version);
  } catch (err) {
    console.error(`[uncompact] Failed to fetch release: ${err.message}`);
    console.error(`[uncompact] You can install manually: go install github.com/${REPO_OWNER}/${REPO_NAME.toLowerCase()}@latest`);
    process.exit(0);
  }

  const asset = release.assets.find((a) => a.name === assetName);
  if (!asset) {
    console.error(`[uncompact] No binary found for ${platform}/${arch} in release ${release.tag_name}`);
    console.error(`[uncompact] Available assets: ${release.assets.map((a) => a.name).join(", ")}`);
    console.error(`[uncompact] You can install manually: go install github.com/${REPO_OWNER}/${REPO_NAME.toLowerCase()}@latest`);
    process.exit(0);
  }

  console.log(`[uncompact] Downloading ${asset.name}...`);

  let buffer;
  try {
    buffer = await httpsGet(asset.browser_download_url);
  } catch (err) {
    console.error(`[uncompact] Failed to download: ${err.message}`);
    process.exit(0);
  }

  console.log(`[uncompact] Extracting...`);

  try {
    if (platform === "windows") {
      extractZip(buffer, binDir, binaryName);
    } else {
      extractTarGz(buffer, binDir, binaryName);
    }
  } catch (err) {
    console.error(`[uncompact] Failed to extract: ${err.message}`);
    process.exit(0);
  }

  if (platform !== "windows") {
    fs.chmodSync(destPath, 0o755);
  }

  console.log(`[uncompact] Installed to ${destPath}`);
}

main().catch((err) => {
  console.error(`[uncompact] Installation failed: ${err.message}`);
  process.exit(0);
});
