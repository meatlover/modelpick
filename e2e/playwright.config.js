// Browser tests of the modelpick page. Run them with `bash scripts/e2e.sh`
// from the repo root: it builds the servers the tests start and sets
// E2E_SERVER and E2E_DEMO.
const { defineConfig, devices } = require("@playwright/test");

module.exports = defineConfig({
  testDir: "./tests",
  retries: 0,
  reporter: "list",
  // A stray test.only would run one test and pass the gate.
  forbidOnly: true,
  use: {
    timezoneId: "America/Caracas",
  },
  projects: [
    {
      name: "chromium",
      use: {
        ...devices["Desktop Chrome"],
        // The tests reach 127.0.0.1 only. No proxy, whatever the shell's
        // HTTP_PROXY/HTTPS_PROXY or the system settings say.
        launchOptions: { args: ["--no-proxy-server"] },
      },
    },
  ],
});
