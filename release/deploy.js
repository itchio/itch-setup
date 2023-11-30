//@ts-check
"use strict";

const { $, cd } = require("@itchio/bob");
const { readdirSync } = require("fs");
const { resolve } = require("path");

/**
 * @param {string[]} _args
 */
async function main(_args) {
  /** @type {string} */
  let channelSuffix;
  /** @type {string} */
  let userVersion;

  if (process.env.CI_COMMIT_TAG) {
    // pushing a stable version
    channelSuffix = "";
    // v9.0.0 => 9.0.0
    userVersion = process.env.CI_COMMIT_TAG.replace(/^v/, "");
  } else if (process.env.CI_COMMIT_REF_NAME === "master") {
    // pushing head
    channelSuffix = "-head";
    userVersion = process.env.CI_COMMIT_SHA || "";
  } else {
    // pushing a branch that isn't master
    console.log(
      `Not pushing non-master branch ${process.env.CI_COMMIT_REF_NAME}`
    );
    return;
  }

  // upload to itch.io
  let toolsDir = resolve(process.cwd(), "tools");
  $(`mkdir -p ${toolsDir}`);
  await cd(toolsDir, async () => {
    let butlerUrl = `https://broth.itch.zone/butler/linux-amd64-head/LATEST/.zip`;
    $(`curl -sLo butler.zip "${butlerUrl}"`);
    $(`unzip butler.zip`);
  });

  $(`${toolsDir}/butler -V`);

  for (let target of ["itch-setup", "kitch-setup"]) {
    await cd(`artifacts/${target}`, async () => {
      let variants = readdirSync(".");
      for (let variant of variants) {
        let channelName = `${variant}${channelSuffix}`;
        let itchTarget = `itchio/${target}:${channelName}`;
        let butlerArgs = [
          "push",
          `--userversion "${userVersion}"`,
          `"${variant}"`,
          `"${itchTarget}"`,
        ];
        $(`${toolsDir}/butler ${butlerArgs.join(" ")}`);
      }
    });
  }
}

main(process.argv.slice(2));
