import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { toast } from "sonner";
import { api } from "@/api/client";
import type { RSSRule } from "@/api/types";
import { CategorySelect } from "@/components/form/category-select";
import { TagPicker } from "@/components/form/tag-picker";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { errorMessage, unwrap } from "@/lib/api";
import { keys, rssQuery, rssRulesQuery } from "@/lib/queries";

/** A new rule, as the server would create it. */
export function newRule(): RSSRule {
  return {
    enabled: true,
    priority: 0,
    useRegex: false,
    mustContain: "",
    mustNotContain: "",
    episodeFilter: "",
    smartFilter: false,
    previouslyMatchedEpisodes: [],
    affectedFeeds: [],
    ignoreDays: 0,
    add: { ratioLimit: -2, seedingTimeLimit: -2, inactiveSeedingTimeLimit: -2 },
  };
}

/** Creates or edits a download rule; name is undefined when closed. */
export function RuleDialog({
  name,
  rule,
  onClose,
}: {
  name: string | undefined;
  rule: RSSRule | undefined;
  onClose: () => void;
}) {
  const qc = useQueryClient();
  const { data: tree } = useQuery(rssQuery);
  const { data: existing = {} } = useQuery(rssRulesQuery);
  const [draft, setDraft] = useState<{ name: string; rule: RSSRule } | null>(null);
  const current = draft ?? { name: name ?? "", rule: rule ?? newRule() };
  const r = current.rule;
  const newName = current.name.trim();
  // Saving under another rule's name would replace it and its history.
  const taken = newName !== name && Object.hasOwn(existing, newName);
  const set = (patch: Partial<RSSRule>) => setDraft({ ...current, rule: { ...r, ...patch } });
  const setAdd = (patch: Partial<RSSRule["add"]>) => set({ add: { ...r.add, ...patch } });
  const close = () => {
    setDraft(null);
    onClose();
  };
  const matches = useQuery({
    queryKey: keys.rssMatches(name ?? ""),
    queryFn: async () =>
      (
        await unwrap(
          api.GET("/rss/rules/{name}/matches", { params: { path: { name: name ?? "" } } }),
        )
      ).items ?? [],
    enabled: !!name,
  });
  const save = useMutation({
    mutationFn: async () => {
      if (name && newName !== name) {
        await unwrap(
          api.POST("/rss/rules/{name}/rename", { params: { path: { name } }, body: { newName } }),
        );
      }
      await unwrap(api.PUT("/rss/rules/{name}", { params: { path: { name: newName } }, body: r }));
    },
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["rss", "rules"] });
      close();
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };
  const feeds = tree?.feeds ?? [];
  const affected = new Set(r.affectedFeeds ?? []);
  const toggleFeed = (url: string, on: boolean) => {
    const next = new Set(affected);
    if (on) {
      next.add(url);
    } else {
      next.delete(url);
    }
    set({ affectedFeeds: [...next] });
  };
  return (
    <Dialog open={name !== undefined} onOpenChange={(open) => !open && close()}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-2xl">
        <form onSubmit={submit} className="grid gap-6">
          <DialogHeader>
            <DialogTitle>{name ? "Edit rule" : "New rule"}</DialogTitle>
            <DialogDescription>
              Terms are wildcards (* and ?), all of which must match, ignoring case; | separates
              alternatives. With regular expressions on, each field is one expression.
            </DialogDescription>
          </DialogHeader>
          <FieldGroup className="gap-5">
            <div className="grid gap-5 sm:grid-cols-[1fr_auto]">
              <Field>
                <FieldLabel htmlFor="rule-name">Name</FieldLabel>
                <Input
                  id="rule-name"
                  value={current.name}
                  onChange={(e) => setDraft({ ...current, name: e.target.value })}
                  required
                  autoFocus={!name}
                  aria-invalid={taken || undefined}
                />
                {taken && (
                  <FieldDescription className="text-destructive">
                    A rule with that name exists.
                  </FieldDescription>
                )}
              </Field>
              <Check label="Enabled" checked={r.enabled} onChange={(enabled) => set({ enabled })} />
            </div>
            <div className="grid gap-5 sm:grid-cols-2">
              <Field>
                <FieldLabel htmlFor="rule-contains">Must contain</FieldLabel>
                <Input
                  id="rule-contains"
                  value={r.mustContain}
                  onChange={(e) => set({ mustContain: e.target.value })}
                  className="font-mono text-xs"
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="rule-not-contains">Must not contain</FieldLabel>
                <Input
                  id="rule-not-contains"
                  value={r.mustNotContain}
                  onChange={(e) => set({ mustNotContain: e.target.value })}
                  className="font-mono text-xs"
                />
              </Field>
            </div>
            <div className="grid gap-5 sm:grid-cols-2">
              <Field>
                <FieldLabel htmlFor="rule-episodes">Episode filter</FieldLabel>
                <Input
                  id="rule-episodes"
                  value={r.episodeFilter}
                  onChange={(e) => set({ episodeFilter: e.target.value })}
                  placeholder="1x2;8-15;20-;"
                  className="font-mono text-xs"
                />
                <FieldDescription>
                  Season, then episodes or ranges, each ending with ;. An open range continues into
                  later seasons.
                </FieldDescription>
              </Field>
              <div className="flex flex-col gap-3 pt-6">
                <Check
                  label="Regular expressions"
                  checked={r.useRegex}
                  onChange={(useRegex) => set({ useRegex })}
                />
                <Check
                  label="Each episode once (smart filter)"
                  checked={r.smartFilter}
                  onChange={(smartFilter) => set({ smartFilter })}
                />
              </div>
            </div>
            <fieldset className="flex flex-col gap-2">
              <legend className="mb-2 text-sm font-medium">Feeds</legend>
              {feeds.length === 0 && <p className="text-sm text-muted-foreground">No feeds yet.</p>}
              {feeds.map((f) => (
                <Check
                  key={f.uid}
                  label={f.path.replaceAll("\\", " / ")}
                  checked={affected.has(f.url)}
                  onChange={(on) => toggleFeed(f.url, on)}
                />
              ))}
            </fieldset>
            <div className="grid gap-5 sm:grid-cols-2">
              <Field>
                <FieldLabel htmlFor="rule-category">Category</FieldLabel>
                <CategorySelect
                  id="rule-category"
                  value={r.add.category ?? ""}
                  onChange={(category) => setAdd({ category })}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="rule-path">Save path</FieldLabel>
                <Input
                  id="rule-path"
                  value={r.add.savePath ?? ""}
                  onChange={(e) => setAdd({ savePath: e.target.value })}
                  placeholder="The category's or the default"
                  className="font-mono text-xs"
                />
              </Field>
            </div>
            <Field>
              <FieldLabel htmlFor="rule-tags">Tags</FieldLabel>
              <TagPicker
                id="rule-tags"
                label="Tags"
                value={r.add.tags ?? []}
                onChange={(tags) => setAdd({ tags })}
              />
            </Field>
            <Check
              label="Add stopped"
              checked={r.add.stopped === true}
              onChange={(stopped) => setAdd({ stopped: stopped || undefined })}
            />
          </FieldGroup>
          {name && (matches.data?.length ?? 0) > 0 && (
            <section className="flex flex-col gap-2 border-t pt-4" aria-label="Matching articles">
              <h3 className="text-sm font-medium">Matching articles, as saved</h3>
              {matches.data?.map((m) => (
                <div key={m.feedUid} className="text-xs">
                  <p className="text-muted-foreground">{m.feedPath.replaceAll("\\", " / ")}</p>
                  <ul className="ml-4 list-disc">
                    {(m.titles ?? []).map((t) => (
                      <li key={t} className="truncate">
                        {t}
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
            </section>
          )}
          <DialogFooter>
            <Button type="submit" size="sm" disabled={!newName || taken || save.isPending}>
              Save
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function Check({
  label,
  checked,
  onChange,
}: {
  label: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
}) {
  return (
    <Label className="flex items-center gap-2.5 font-normal normal-case">
      <Checkbox checked={checked} onCheckedChange={(v) => onChange(v === true)} />
      {label}
    </Label>
  );
}
