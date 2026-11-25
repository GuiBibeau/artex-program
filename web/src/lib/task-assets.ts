import { t } from "@/i18n";
import type { NewAssetType, SolanaAssetType } from "@/lib/types";

export const SOLANA_ASSET_TYPES: readonly SolanaAssetType[] = [
  "program",
  "instruction",
  "account",
  "authority",
  "mint",
  "token_account",
];

export function isSolanaAssetType(type: string): type is SolanaAssetType {
  return (SOLANA_ASSET_TYPES as readonly string[]).includes(type);
}

const ASSET_TYPE_LABELS: Record<NewAssetType, string> = {
  account: t("Account"),
  app: t("App"),
  authority: t("Authority"),
  endpoint: t("Endpoint"),
  instruction: t("Instruction"),
  ip: "IP",
  mint: t("Mint"),
  program: t("Program"),
  root_domain: t("Root domain"),
  service: t("Service"),
  subdomain: t("Subdomain"),
  token_account: t("Token account"),
};

const TASK_ASSET_SOURCE_LABELS: Record<string, string> = {
  agent: t("Agent-discovered"),
  anchor: t("Blackboard anchor"),
  api: t("Assets API"),
  company: t("Company association"),
  legacy: t("Legacy association"),
  manual: t("Manually added"),
  system: t("System association"),
  task: t("Task initialization"),
};

export function taskAssetTypeLabel(type: NewAssetType): string {
  return ASSET_TYPE_LABELS[type];
}

// The text that names a Solana asset in a list: the instruction name, or the
// display label when one was given, else the on-chain address.
export function solanaAssetName(a: { type: string; address?: string; name?: string; parent?: string }): string {
  if (a.type === "instruction" && a.name) return `${shortAddress(a.parent)}::${a.name}`;
  // The backend returns "" for an absent field, so empty strings fall through too.
  return [a.name, a.address].find((value) => Boolean(value?.trim())) ?? "—";
}

// A base58 key is 32 to 44 characters; lists show its head and tail.
export function shortAddress(addr?: string): string {
  if (!addr) return "—";
  return addr.length > 12 ? `${addr.slice(0, 4)}…${addr.slice(-4)}` : addr;
}

export function taskAssetSourceLabel(source: string): string {
  return TASK_ASSET_SOURCE_LABELS[source] ?? source;
}
