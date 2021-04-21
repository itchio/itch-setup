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
   *   arch: "i686" | "x86_64",
   *   target: "itch-setup" | "kitch-setup" | "missing",
   *   userSpecifiedOS?: boolean,
   *   userSpecifiedArch?: boolean,
   * }}
   */
  let opts = {
    os: detectOS(),
    arch: DEFAULT_ARCH,
    target: "missing",
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

      if (k === "os" || k === "arch" || k === "target") {
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
          if (v === "i686" || v === "x86_64") {
            opts.arch = v;
            opts.userSpecifiedArch = true;
          } else {
            throw new Error(`Unsupported arch ${chalk.yellow(v)}`);
          }
        } else if (k === "target") {
          if (v === "itch-setup" || v === "kitch-setup") {
            opts.target = v;
          } else {
            throw new Error(`Unsupported target ${chalk.yellow(v)}`);
          }
        }
      } else {
        throw new Error(`Unknown option ${chalk.yellow(arg)}`);
      }
    }
  }

  if (opts.target === "missing") {
    throw new Error(`Must specify ${chalk.yellow("--target")}`);
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
  if (process.env.CI_BUILD_TAG) {
    version = process.env.CI_BUILD_TAG;
  } else if (
    process.env.CI_BUILD_REF_NAME &&
    process.env.CI_BUILD_REF_NAME !== "master"
  ) {
    version = process.env.CI_BUILD_REF_NAME;
  }
  let buildRef = process.env.CI_BUILD_REF || "no-commit";

  let builtAt = $$("date +%s");
  let ldFlags = `-X main.version=${version} -X main.builtAt=${builtAt} -X main.commit=${buildRef} -X main.target=${opts.target} -w -s`;
  if (opts.os === "windows") {
    ldFlags += ` -H windowsgui -extldflags=-static`;
  }

  setenv(`CI_LDFLAGS`, ldFlags);

  let target = opts.target;
  if (opts.os === "windows") {
    target += ".exe";
  }

  if (opts.os === "windows") {
    $("windres -o itch-setup.syso itch-setup.rc");
    $("file itch-setup.syso");
  }

  let goTags = "";
  if (opts.os === "linux") {
    goTags = `-tags gtk_3_14`;
  }

  if (opts.os === "darwin") {
    setenv(`CGO_CFLAGS`, `-mmacosx-version-min=10.10`);
    setenv(`CGO_LDFLAGS`, `-mmacosx-version-min=10.10`);
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

    console.log(`Signing Windows binary...`);
    let signArgs = [
      `sign`, // verb
      `//v`, // verbose
      `//s MY`, // store
      `//n "itch corp"`, // name
      `//fd sha256`, // file digest algo (default is SHA-1)
      `//tr http://timestamp.comodoca.com/?td=sha256`, // URL of RFC 3161 timestamp server
      `//td sha256`, // timestamp digest algo
      '//a', // choose best cert
      target,
    ];
    $(`tools/signtool.exe ${signArgs.join(" ")}`);
  }

  if (opts.os === "darwin") {
    console.log(`Signing macOS binary...`);
    let signKey = "Developer ID Application: itch corp. (AK2D34UPD2)";
    $(`codesign --deep --force --verbose --sign "${signKey}" "${target}"`);
    $(`codesign --verify -vvvv "${target}"`);
  }

  let binaries = `artifacts/${opts.target}/${opts.os}-${goArch}`;
  $(`mkdir -p ${binaries}`);
  $(`cp -rf ${target} ${binaries}/`);
}

/**
 * @param {"i686" | "x86_64"} arch
 * @returns {"386" | "amd64"}
 */
function archToGoArch(arch) {
  switch (arch) {
    case "i686":
      return "386";
    case "x86_64":
      return "amd64";
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
