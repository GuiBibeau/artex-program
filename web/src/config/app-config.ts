import packageJson from "../../package.json";

import { t } from "@/i18n";

const currentYear = new Date().getFullYear();

export const APP_CONFIG = {
  name: "ARTEX Program",
  version: packageJson.version,
  copyright: `© ${currentYear}, ARTEX.`,
  meta: {
    title: t("ARTEX Program — Solana Program Security Console"),
    description: t("Console for the LLM-driven autonomous Solana program security testing system"),
  },
};
