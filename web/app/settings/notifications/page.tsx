"use client";

import { useEffect, useState } from "react";
import { Button, Card, Label, Switch, toast } from "@heroui/react";
import { api, type Account } from "@/lib/api";

const prefs = [
  { key: "digest", label: "Weekly digest", desc: "Summary of what your agents learned this week" },
  { key: "disputes", label: "Dispute alerts", desc: "When an agent disputes a memory's accuracy" },
  { key: "consolidation", label: "Consolidation reports", desc: "Results after each memory cleanup run" },
  { key: "expiry", label: "Expiry warnings", desc: "Before important memories reach their TTL" },
];

export default function NotificationsPage() {
  const [account, setAccount] = useState<Account | null>(null);
  const [values, setValues] = useState<Record<string, boolean>>({});
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    api
      .account()
      .then((a) => {
        setAccount(a);
        setValues(a.notifications ?? {});
      })
      .catch(() => setAccount({}));
  }, []);

  async function save() {
    setBusy(true);
    try {
      await api.saveAccount({ ...account, notifications: values });
      toast("Notification preferences saved", { variant: "success" });
    } catch (e) {
      toast(e instanceof Error ? e.message : "Save failed", { variant: "danger" });
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex max-w-2xl flex-col gap-3">
      <div>
        <h2 className="text-lg font-medium">Notifications</h2>
        <p className="text-sm text-muted-foreground">
          Preferences are stored on your hub. Delivery channels arrive with the hosted tier.
        </p>
      </div>
      <Card>
        <Card.Content className="flex flex-col gap-1">
          {prefs.map((p, i) => (
            <div
              key={p.key}
              className={`flex items-center justify-between gap-4 py-3 ${
                i < prefs.length - 1 ? "border-b border-separator" : ""
              }`}
            >
              <div className="flex flex-col">
                <Label>{p.label}</Label>
                <span className="text-xs text-muted-foreground">{p.desc}</span>
              </div>
              <Switch
                isSelected={values[p.key] ?? false}
                onChange={(v: boolean) => setValues((s) => ({ ...s, [p.key]: v }))}
              />
            </div>
          ))}
          <div className="pt-3">
            <Button isDisabled={busy || !account} onPress={save}>
              Save preferences
            </Button>
          </div>
        </Card.Content>
      </Card>
    </div>
  );
}
