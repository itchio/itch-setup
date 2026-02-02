//@ts-check
"use strict";

const {
  $,
  $$,
  header,
  detectOS,
  setVerbose,
  chalk,
  debug,
  setenv,
  cd,
} = require("@itchio/bob");

const DEFAULT_ARCH = "x86_64";

/**
 * @typedef OsInfo
 * @type {{
 *   architectures: {
 *     [key: string]: {
 *       prependPath?: string,
 *     }
 *   }
 * }}
 */

/**
 * @type {{[name: string]: OsInfo}}
 */
const OS_INFOS = {
  windows: {
    architectures: {
      i686: {
        prependPath: "/mingw32/bin",
      },
      x86_64: {
        prependPath: "/mingw64/bin",
      },
    },
  },
  linux: {
    architectures: {
      x86_64: {},
    },
  },
  darwin: {
    architectures: {
      x86_64: {},
      arm64: {},
    },
  },
};

/**
 * @param {string[]} args
 */
async function main(args) {
  header("Gathering configuration");

  /**
   * @type {{
   *   os: "linux" | "windows" | "darwin",
   *   arch: "i686" | "x86_64" | "arm64",
   *   userSpecifiedOS?: boolean,
   *   userSpecifiedArch?: boolean,
   * }}
   */
  let opts = {
    os: detectOS(),
    arch: DEFAULT_ARCH,
  };

  for (let i = 0; i < args.length; i++) {
    let arg = args[i];

    let matches = /^--(.*)$/.exec(arg);
    if (matches) {
      let k = matches[1];
      if (k == "verbose") {
        setVerbose(true);
        continue;
      }

      if (k === "os" || k === "arch") {
        i++;
        let v = args[i];

        if (k === "os") {
          if (v === "linux" || v === "windows" || v === "darwin") {
            opts.os = v;
            opts.userSpecifiedOS = true;
          } else {
            throw new Error(`Unsupported os ${chalk.yellow(v)}`);
          }
        } else if (k === "arch") {
          if (v === "i686" || v === "x86_64" || v === "arm64") {
            opts.arch = v;
            opts.userSpecifiedArch = true;
          } else {
            throw new Error(`Unsupported arch ${chalk.yellow(v)}`);
          }
        }
      } else {
        throw new Error(`Unknown option ${chalk.yellow(arg)}`);
      }
    }
  }

  if (opts.userSpecifiedOS) {
    console.log(`Using user-specified OS ${chalk.yellow(opts.os)}`);
  } else {
    console.log(
      `Using detected OS ${chalk.yellow(opts.os)} (use --os to override)`
    );
  }

  if (opts.userSpecifiedArch) {
    console.log(`Using user-specified arch ${chalk.yellow(opts.arch)}`);
  } else {
    console.log(
      `Using detected arch ${chalk.yellow(opts.arch)} (use --arch to override)`
    );
  }

  let osInfo = OS_INFOS[opts.os];
  debug({ osInfo });
  if (!osInfo) {
    throw new Error(`Unsupported OS ${chalk.yellow(opts.os)}`);
  }

  let archInfo = osInfo.architectures[opts.arch];
  debug({ archInfo });
  if (!archInfo) {
    throw new Error(`Unsupported arch '${opts.arch}' for os '${opts.os}'`);
  }
  let goArch = archToGoArch(opts.arch);

  if (archInfo.prependPath) {
    if (opts.os === "windows") {
      let prependPath = $$(`cygpath -w ${archInfo.prependPath}`).trim();
      console.log(
        `Prepending ${chalk.yellow(archInfo.prependPath)} (aka ${chalk.yellow(
          prependPath
        )}) to $PATH`
      );
      process.env.PATH = `${prependPath};${process.env.PATH}`;
    } else {
      console.log(`Prepending ${chalk.yellow(archInfo.prependPath)} to $PATH`);
      process.env.PATH = `${archInfo.prependPath}:${process.env.PATH}`;
    }
  }

  header("Showing tool versions");
  $(`node --version`);
  $(`go version`);

  if (opts.userSpecifiedArch) {
    await cd("node_modules/@itchio/husk", async () => {
      $(`npm run postinstall -- --verbose --arch ${opts.arch}`);
    });
  }

  let version = "head";
  if (process.env.GITHUB_REF_TYPE === "tag") {
    version = process.env.GITHUB_REF_NAME;
  } else if (
    process.env.GITHUB_REF_NAME &&
    process.env.GITHUB_REF_NAME !== "master"
  ) {
    version = process.env.GITHUB_REF_NAME;
  }
  let buildRef = process.env.GITHUB_SHA || "no-commit";

  let builtAt = $$("date +%s");
  let ldFlags = `-X main.version=${version} -X main.builtAt=${builtAt} -X main.commit=${buildRef} -w -s`;
  if (opts.os === "windows") {
    ldFlags += ` -H windowsgui -extldflags=-static`;
  }

  setenv(`CI_LDFLAGS`, ldFlags);

  let target = "itch-setup";
  if (opts.os === "windows") {
    target += ".exe";
  }

  if (opts.os === "windows") {
    $("windres -o itch-setup.syso itch-setup.rc");
    $("file itch-setup.syso");
  }

  let goTags = "";
  if (opts.os === "linux") {
    // NOTE: we are actually on gtk 3.24 but it doesn't work for Debian buster's specific version: https://github.com/gotk3/gotk3/issues/671#issuecomment-798590357
    goTags = `-tags "pango_1_42 gtk_3_22 glib_2_58 gdk_pixbuf_2_38"`;
  }

  if (opts.os === "darwin") {
    // arm64 requires macOS 11.0+, x86_64 can target 10.10+
    let minVersion = opts.arch === "arm64" ? "11.0" : "10.10";
    setenv(`CGO_CFLAGS`, `-mmacosx-version-min=${minVersion}`);
    setenv(`CGO_LDFLAGS`, `-mmacosx-version-min=${minVersion}`);
  }

  setenv(`GOOS`, opts.os);
  setenv(`GOARCH`, archToGoArch(opts.arch));
  setenv(`CGO_ENABLED`, "1");

  header("Building native code");
  // -a is necessary to bypass the cache because Go *will* cache
  // the result of compiling nmac, but it won't detect that `nmac.m`
  // changed, so screw us I guess.
  $(`go build -a -ldflags "${ldFlags}" ${goTags} -o ${target}`);
  $(`file ${target}`);

  if (opts.os === "windows") {
    verifyCoIncrementMTAUsage(target);
  }

  let binaries = `artifacts/itch-setup/${opts.os}-${goArch}`;
  $(`mkdir -p ${binaries}`);
  $(`cp -rf ${target} ${binaries}/`);
}

/**
 * @param {"i686" | "x86_64" | "arm64"} arch
 * @returns {"386" | "amd64" | "arm64"}
 */
function archToGoArch(arch) {
  switch (arch) {
    case "i686":
      return "386";
    case "x86_64":
      return "amd64";
    case "arm64":
      return "arm64";
    default:
      throw new Error(`unsupported arch: ${chalk.yellow(arch)}`);
  }
}

/**
 * @param {string} target
 */
function verifyCoIncrementMTAUsage(target) {
  console.log(`Verifying that we don't rely on CoIncrementMTAUsage`);
  let lines = $$(
    `objdump --private-headers "${target}" | grep -E "[0-9]+  Co[A-Z]"`
  ).split("\n");
  let comMethods = [];
  for (let line of lines) {
    line = line.trim();
    if (line == "") {
      continue;
    }
    let matches = /[^ ]+$/.exec(line);
    if (matches) {
      let method = matches[0];
      comMethods.push(method);
    } else {
      console.log(chalk.yellow(`Could not parse line: ${line}`));
    }
  }
  console.log(`Found COM methods ${comMethods.join(", ")}`);
  if (comMethods.indexOf("CoIncrementMTAUsage") !== -1) {
    console.log(
      chalk.magenta(
        "Check failed: husk cannot depend on CoIncrementMTAUsage, as it breaks Windows 7 compatibility."
      )
    );
    process.exit(1);
  }
}

main(process.argv.slice(2));
