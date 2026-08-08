"use client";

import { useState } from "react";
import { Button, Card, Chip, toast } from "@heroui/react";
import { api, type ConsolidateReport } from "@/lib/api";

export default function MaintenancePage() {
  const [report, setReport] = useState<ConsolidateReport | null>(null);
  const [busy, setBusy] = useState(false);

  async function consolidate(dryRun: boolean) {
    setBusy(true);
    try {
      const rep = await api.consolidate("", dryRun);
      setReport(rep);
      if (!dryRun)
        toast(`Consolidated: ${rep.merged} merged, ${rep.archived} archived`, { variant: "success" });
    } catch (e) {
      toast(e instanceof Error ? e.message : "Consolidation failed", { variant: "danger" });
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex max-w-2xl flex-col gap-3">
      <div>
        <h2 className="text-lg font-medium">Consolidation</h2>
        <p className="text-sm text-muted-foreground">
          Asks the hub&apos;s LLM to merge duplicates, resolve contradictions, and archive stale
          memories. Sources are archived and linked, never deleted. Requires EVERMEMO_LLM_URL on
          the hub.
        </p>
      </div>
      <Card>
        <Card.Content className="flex flex-col gap-4">
          <div className="flex gap-2">
            <Button isDisabled={busy} variant="secondary" onPress={() => consolidate(true)}>
              Dry run
            </Button>
            <Button isDisabled={busy} variant="danger-soft" onPress={() => consolidate(false)}>
              Run consolidation
            </Button>
          </div>

          {report && (
            <div className="rounded-lg bg-surface-secondary p-3 text-sm">
              <div className="mb-2 flex gap-2">
                <Chip size="sm" variant="soft">
                  {report.dry_run ? "dry run" : "applied"}
                </Chip>
                <span className="text-muted-foreground">
                  reviewed {report.reviewed} · merged {report.merged} · archived {report.archived} ·
                  kept {report.kept}
                </span>
              </div>
              {report.actions?.map((a, i) => (
                <div key={i} className="font-mono text-xs text-muted-foreground">
                  {a.action}: {a.ids.join(", ")} {a.reason ? `— ${a.reason}` : ""}
                </div>
              ))}
            </div>
          )}
        </Card.Content>
      </Card>
    </div>
  );
}
