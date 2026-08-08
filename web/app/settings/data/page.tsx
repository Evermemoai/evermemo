"use client";

import { useRef, useState } from "react";
import { Button, Card, toast } from "@heroui/react";
import { api } from "@/lib/api";

export default function DataPage() {
  const [busy, setBusy] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);

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

  return (
    <div className="flex max-w-2xl flex-col gap-3">
      <div>
        <h2 className="text-lg font-medium">Backup &amp; restore</h2>
        <p className="text-sm text-muted-foreground">
          Exports skip expired/archived memories. Import preserves ids (idempotent).
        </p>
      </div>
      <Card>
        <Card.Content className="flex flex-col gap-4">
          <div className="flex flex-wrap gap-2">
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
          <p className="text-xs text-muted-foreground">
            Tip: for a byte-exact snapshot including links and votes, run{" "}
            <code className="font-mono">evermemo backup</code> on the hub.
          </p>
        </Card.Content>
      </Card>
    </div>
  );
}
