import { Tag, Tooltip } from "antd";
import { useTranslation } from "react-i18next";
import type { RowDraftStateLookup } from "./sections";

// ResourceStatusTag renders the per-row status tag shared by every resource
// table: a draft badge (new / modified / deleted) when the row is touched by
// the active draft, otherwise the published runtime status.
export function ResourceStatusTag({
  resource,
  row,
  rowDraftState,
}: {
  resource: string;
  row: { id?: string; status?: string };
  rowDraftState: RowDraftStateLookup;
}) {
  const { t } = useTranslation();
  const state = rowDraftState(resource, row);
  if (state && state !== "unchanged") {
    const label =
      state === "new"
        ? t("resources.draftNew")
        : state === "modified"
          ? t("resources.draftModified")
          : t("resources.draftDeleted");
    const color =
      state === "new" ? "blue" : state === "modified" ? "orange" : "red";
    return (
      <Tooltip title={t("resources.draftStatusHint")}>
        <Tag color={color}>{label}</Tag>
      </Tooltip>
    );
  }
  const value = row.status;
  const label = value
    ? value === "catalog"
      ? t("resources.catalog")
      : value
    : t("resources.published");
  const color =
    value === "active" || value === "enabled" || !value ? "green" : "default";
  return <Tag color={color}>{label}</Tag>;
}
