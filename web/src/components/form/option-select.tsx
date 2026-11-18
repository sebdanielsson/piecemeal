import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

export interface Option<T extends string> {
  value: T;
  label: string;
}

/** A Select over a fixed list of string options. */
export function OptionSelect<T extends string>({
  id,
  value,
  onChange,
  options,
  disabled,
  className,
  placeholder,
  "aria-label": ariaLabel,
}: {
  id?: string;
  value: T;
  onChange: (value: T) => void;
  options: readonly Option<T>[];
  disabled?: boolean;
  className?: string;
  placeholder?: string;
  "aria-label"?: string;
}) {
  return (
    <Select
      items={options}
      value={value}
      onValueChange={(v) => v !== null && onChange(v as T)}
      disabled={disabled}
    >
      <SelectTrigger id={id} className={className ?? "w-full"} aria-label={ariaLabel}>
        <SelectValue placeholder={placeholder} />
      </SelectTrigger>
      <SelectContent>
        {options.map((o) => (
          <SelectItem key={o.value} value={o.value}>
            {o.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
