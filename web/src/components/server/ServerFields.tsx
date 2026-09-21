import type { EggImage, EggSpec, EggVariable } from "../../lib/types";
import { useT } from "../../lib/i18n";
import { Field, Input } from "../ui/Input";
import type { ByteQuantity, ByteUnit } from "../../lib/quantity";

export function editableVariables(spec: EggSpec | undefined): EggVariable[] {
  return (spec?.variables ?? []).filter((v) => v.userEditable && v.name !== "EULA");
}

export function variableProblem(v: EggVariable, value: string): "required" | "invalid" | null {
  if (value === "") return v.required ? "required" : null;
  if (!v.validationRegex) return null;
  try {
    return new RegExp(v.validationRegex).test(value) ? null : "invalid";
  } catch {
    return null;
  }
}

export function VariableFields({
  variables,
  values,
  onChange,
  disabled,
  idPrefix,
}: {
  variables: EggVariable[];
  values: Record<string, string>;
  onChange: (name: string, value: string) => void;
  disabled?: boolean;
  idPrefix: string;
}) {
  const t = useT();
  return (
    <>
      {variables.map((v) => {
        const value = values[v.name] ?? "";
        const problem = variableProblem(v, value);
        return (
          <Field key={v.name} label={`${v.name}${v.required ? ` (${t("dashboard.required")})` : ""}`} htmlFor={`${idPrefix}-${v.name}`}>
            <Input id={`${idPrefix}-${v.name}`} value={value} disabled={disabled} onChange={(e) => onChange(v.name, e.target.value)} />
            {v.description && <span className="font-prose text-xs text-text-tertiary">{v.description}</span>}
            {problem && !disabled && (
              <span className="font-sans text-xs text-status-failed">
                {problem === "required" ? t("dashboard.required") : t("dashboard.invalidValue")}
              </span>
            )}
          </Field>
        );
      })}
    </>
  );
}

export function ByteInput({
  id,
  value,
  onChange,
  disabled,
}: {
  id: string;
  value: ByteQuantity;
  onChange: (v: ByteQuantity) => void;
  disabled?: boolean;
}) {
  return (
    <div className="flex gap-2">
      <Input
        id={id}
        type="number"
        min={1}
        step="any"
        className="min-w-0 grow"
        value={Number.isFinite(value.value) ? value.value : ""}
        disabled={disabled}
        onChange={(e) => onChange({ ...value, value: e.target.valueAsNumber })}
        required
      />
      <select
        aria-label="unit"
        value={value.unit}
        disabled={disabled}
        onChange={(e) => onChange({ ...value, unit: e.target.value as ByteUnit })}
        className="rounded-lg border border-border-strong bg-surface px-3 py-2 font-sans text-sm text-text-primary focus:outline-none focus:ring-2 focus:ring-primary"
      >
        <option value="Mi">Mi</option>
        <option value="Gi">Gi</option>
      </select>
    </div>
  );
}

export function CpuInput({
  id,
  value,
  onChange,
  disabled,
}: {
  id: string;
  value: string;
  onChange: (v: string) => void;
  disabled?: boolean;
}) {
  return (
    <Input
      id={id}
      type="number"
      min={0.1}
      step="any"
      value={value}
      disabled={disabled}
      onChange={(e) => onChange(e.target.value)}
      required
    />
  );
}

export function ImageSelect({
  id,
  images,
  value,
  onChange,
  disabled,
}: {
  id: string;
  images: EggImage[];
  value: string;
  onChange: (name: string) => void;
  disabled?: boolean;
}) {
  return (
    <select
      id={id}
      value={value}
      disabled={disabled}
      onChange={(e) => onChange(e.target.value)}
      className="rounded-lg border border-border-strong bg-surface px-3 py-2 font-sans text-sm text-text-primary focus:outline-none focus:ring-2 focus:ring-primary"
    >
      {images.map((img) => (
        <option key={img.name} value={img.name} title={img.image}>
          {img.name}
        </option>
      ))}
    </select>
  );
}
