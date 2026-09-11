import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import enUS from "./locales/en-US/common.json";
import zhCN from "./locales/zh-CN/common.json";
import enRequests from "./locales/en-US/requests.json";
import zhRequests from "./locales/zh-CN/requests.json";
import enSetup from "./locales/en-US/setup.json";
import zhSetup from "./locales/zh-CN/setup.json";
import enOps from "./locales/en-US/ops.json";
import zhOps from "./locales/zh-CN/ops.json";
import enGovernance from "./locales/en-US/governance.json";
import zhGovernance from "./locales/zh-CN/governance.json";
const storedLanguage =
  typeof localStorage !== "undefined"
    ? localStorage.getItem("liteaig-lang")
    : null;
void i18n.use(initReactI18next).init({
  resources: {
    "en-US": {
      common: enUS,
      requests: enRequests,
      setup: enSetup,
      ops: enOps,
      governance: enGovernance,
    },
    "zh-CN": {
      common: zhCN,
      requests: zhRequests,
      setup: zhSetup,
      ops: zhOps,
      governance: zhGovernance,
    },
  },
  lng:
    storedLanguage === "en-US" || storedLanguage === "zh-CN"
      ? storedLanguage
      : "zh-CN",
  fallbackLng: "en-US",
  defaultNS: "common",
  interpolation: { escapeValue: false },
});
i18n.on("languageChanged", (language) => {
  try {
    localStorage.setItem("liteaig-lang", language);
  } catch {
    // Storage unavailable (private mode); the choice simply does not persist.
  }
});
export default i18n;
