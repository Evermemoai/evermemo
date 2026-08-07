"use client";

import { useEffect, useRef, useState } from "react";
import { Button, Card, Chip, Label, TextField, Input, toast } from "@heroui/react";
import { api, getSettings, saveSettings, type ConsolidateReport } from "@/lib/api";

export default function SettingsPage() {
  const [hubUrl, setHubUrl] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [agent, setAgent] = useState("");
  const [report, setReport] = useState<ConsolidateReport | null>(null);
  const [busy, setBusy] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const s = getSettings();
    setHubUrl(s.hubUrl);
    setApiKey(s.apiKey);
    setAgent(s.agent);
  }, []);

  function save() {
    saveSettings({ hubUrl: hubUrl.trim(), apiKey: apiKey.trim(), agent: agent.trim() });
    toast("Settings saved", { variant: "success" });
  }

  async function testConnection() {
    save();
    try {
      const h = await api.health();
      toast(`Connected — ${h.memories} memories on the hub`, { variant: "success" });
    } catch (e) {
      toast(e instanceof Error ? e.message : "Connection failed", { variant: "danger" });
    }
  }

  async function doExport() {
    try {
      const blob = await api.exportJSONL();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `evermemo-export-${new Date().toISOString().slice(0, 10)}.jsonl`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (e) {
      toast(e instanceof Error ? e.message : "Export failed", { variant: "danger" });
    }
  }

  async function doImport(file: File) {
    setBusy(true);
    try {
      const n = await api.importJSONL(file);
      toast(`Imported ${n} memories`, { variant: "success" });
    } catch (e) {
      toast(e instanceof Error ? e.message : "Import failed", { variant: "danger" });
    } finally {
      setBusy(false);
      if (fileRef.current) fileRef.current.value = "";
    }
  }

  async function consolidate(dryRun: boolean) {
    setBusy(true);
    try {
      const rep = await api.consolidate("", dryRun);
      setReport(rep);
      if (!dryRun) toast(`Consolidated: ${rep.merged} merged, ${rep.archived} archived`, { variant: "success" });
    } catch (e) {
      toast(e instanceof Error ? e.message : "Consolidation failed", { variant: "danger" });
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Settings</h1>
        <p className="text-sm text-muted-foreground">Hub connection, data, and maintenance</p>
      </div>

      <h2 className="text-lg font-medium">Connection</h2>
      <Card className="[--surface:#232323]">
        <Card.Content className="flex flex-col gap-4 py-5">
          <TextField value={hubUrl} onChange={setHubUrl}>
            <Label>Hub URL</Label>
            <Input placeholder="http://localhost:7777" />
          </TextField>
          <TextField type="password" value={apiKey} onChange={setApiKey}>
            <Label>API key</Label>
            <Input placeholder="your agent key" />
          </TextField>
          <TextField value={agent} onChange={setAgent}>
            <Label>Agent name</Label>
            <Input placeholder="dashboard" />
          </TextField>
          <div className="flex gap-2">
            <Button onPress={save}>Save</Button>
            <Button variant="secondary" onPress={testConnection}>
              Test connection
            </Button>
          </div>
        </Card.Content>
      </Card>

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        <div className="flex flex-col gap-3">
          <h2 className="text-lg font-medium">Data</h2>
          <Card className="flex-1 [--surface:#232323]">
            <Card.Content className="flex h-full flex-col gap-4 py-5">
              <div className="flex flex-col gap-1">
                <h3 className="text-sm font-medium">Backup &amp; restore</h3>
                <span className="text-xs text-muted-foreground">
                  Exports skip expired/archived memories. Import preserves ids (idempotent).
                </span>
              </div>
              <div className="mt-auto flex flex-wrap gap-2">
                <Button variant="secondary" onPress={doExport}>
                  Export JSONL
                </Button>
                <Button isDisabled={busy} variant="secondary" onPress={() => fileRef.current?.click()}>
                  Import JSONL
                </Button>
              </div>
              <input
                ref={fileRef}
                hidden
                accept=".jsonl,.ndjson,.json"
                type="file"
                onChange={(e) => e.target.files?.[0] && doImport(e.target.files[0])}
              />
            </Card.Content>
          </Card>
        </div>

        <div className="flex flex-col gap-3">
          <h2 className="text-lg font-medium">Maintenance</h2>
          <Card className="flex-1 [--surface:#232323]">
            <Card.Content className="flex h-full flex-col gap-4 py-5">
              <div className="flex flex-col gap-1">
                <h3 className="text-sm font-medium">Consolidation</h3>
                <p className="text-xs text-muted-foreground">
                  Consolidation asks the hub&apos;s LLM to merge duplicates, resolve contradictions, and
                  archive stale memories. Sources are archived and linked, never deleted. Requires
                  EVERMEMO_LLM_URL on the hub.
                </p>
              </div>
              <div className="mt-auto flex gap-2">
                <Button isDisabled={busy} variant="secondary" onPress={() => consolidate(true)}>
                  Dry run
                </Button>
                <Button isDisabled={busy} variant="danger-soft" onPress={() => consolidate(false)}>
                  Run consolidation
                </Button>
              </div>
            </Card.Content>
          </Card>
        </div>
      </div>

      {report && (
        <div className="rounded-lg bg-surface-secondary p-3 text-sm">
          <div className="mb-2 flex gap-2">
            <Chip size="sm" variant="soft">{report.dry_run ? "dry run" : "applied"}</Chip>
            <span className="text-muted-foreground">
              reviewed {report.reviewed} · merged {report.merged} · archived {report.archived} · kept {report.kept}
            </span>
          </div>
          {report.actions?.map((a, i) => (
            <div key={i} className="font-mono text-xs text-muted-foreground">
              {a.action}: {a.ids.join(", ")} {a.reason ? `— ${a.reason}` : ""}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
