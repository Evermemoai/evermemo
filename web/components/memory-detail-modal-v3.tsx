"use client";

import { useEffect, useState } from "react";
import { Button, Chip, Input, ListBox, Modal, Select, Separator, TextField, toast } from "@heroui/react";
import { api, type Memory } from "@/lib/api";

const RELS = ["relates_to", "supersedes", "derived_from"] as const;

// Detail modal: full memory, links, verify (confirm/dispute), delete.
export function MemoryDetailModal({
  memoryId,
  isOpen,
  onOpenChange,
  onChanged,
}: {
  memoryId: string | null;
  isOpen: boolean;
  onOpenChange: (open: boolean) => void;
  onChanged: () => void;
}) {
  const [mem, setMem] = useState<Memory | null>(null);
  const [busy, setBusy] = useState(false);
  const [linkRel, setLinkRel] = useState<string>("relates_to");
  const [linkTo, setLinkTo] = useState("");

  useEffect(() => {
    if (isOpen && memoryId) {
      api.get(memoryId).then(setMem).catch(() => setMem(null));
    } else {
      setMem(null);
    }
  }, [isOpen, memoryId]);

  async function verify(vote: "confirm" | "dispute") {
    if (!mem) return;
    setBusy(true);
    try {
      const updated = await api.verify(mem.id, vote);
      setMem({ ...mem, confidence: updated.confidence });
      toast(`Recorded ${vote} — confidence now ${Math.round(updated.confidence * 100)}%`, {
        variant: vote === "confirm" ? "success" : "warning",
      });
      onChanged();
    } catch (e) {
      toast(e instanceof Error ? e.message : "Verify failed", { variant: "danger" });
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    if (!mem) return;
    setBusy(true);
    try {
      await api.remove(mem.id);
      toast("Memory deleted", { variant: "success" });
      onOpenChange(false);
      onChanged();
    } catch (e) {
      toast(e instanceof Error ? e.message : "Delete failed", { variant: "danger" });
    } finally {
      setBusy(false);
    }
  }

  async function addLink() {
    if (!mem || !linkTo.trim()) return;
    setBusy(true);
    try {
      await api.link(mem.id, linkRel, linkTo.trim());
      const fresh = await api.get(mem.id);
      setMem(fresh);
      setLinkTo("");
      toast(`Linked ${linkRel} → ${linkTo.trim()}`, { variant: "success" });
      onChanged();
    } catch (e) {
      toast(e instanceof Error ? e.message : "Link failed", { variant: "danger" });
    } finally {
      setBusy(false);
    }
  }

  const confidenceColor =
    (mem?.confidence ?? 0) >= 0.7 ? "success" : (mem?.confidence ?? 0) >= 0.4 ? "warning" : "danger";

  return (
    <Modal isOpen={isOpen} onOpenChange={onOpenChange}>
      <Modal.Backdrop>
        <Modal.Container>
          <Modal.Dialog className="sm:max-w-[560px]">
            <Modal.CloseTrigger />
            <Modal.Header>
              <Modal.Heading className="font-mono text-sm text-muted-foreground">
                {mem?.id ?? "…"}
              </Modal.Heading>
            </Modal.Header>
            <Modal.Body className="flex flex-col gap-4">
              <p className="text-base leading-relaxed">{mem?.content}</p>

              <div className="flex flex-wrap items-center gap-2">
                {mem && (
                  <Chip color={confidenceColor} size="sm" variant="soft">
                    {Math.round(mem.confidence * 100)}% confidence
                  </Chip>
                )}
                {mem?.agent && (
                  <Chip color="accent" size="sm" variant="soft">
                    by {mem.agent}
                  </Chip>
                )}
                {mem && (
                  <Chip size="sm" variant="soft">
                    {mem.namespace}
                  </Chip>
                )}
                {mem?.tags?.map((t) => (
                  <Chip key={t} size="sm">
                    {t}
                  </Chip>
                ))}
              </div>

              <div className="text-xs text-muted-foreground">
                Created {mem ? new Date(mem.created_at).toLocaleString() : ""}
                {mem?.expires_at ? ` · expires ${new Date(mem.expires_at).toLocaleString()}` : ""}
              </div>

              {mem?.links && mem.links.length > 0 && (
                <>
                  <Separator />
                  <div className="flex flex-col gap-1.5">
                    <span className="text-xs font-medium uppercase text-muted-foreground">Links</span>
                    {mem.links.map((l, i) => (
                      <div key={i} className="font-mono text-xs text-muted-foreground">
                        {l.from === mem.id ? "this" : l.from}{" "}
                        <span className="text-accent">—{l.rel}→</span>{" "}
                        {l.to === mem.id ? "this" : l.to}
                      </div>
                    ))}
                  </div>
                </>
              )}

              <Separator />
              <div className="flex flex-col gap-2">
                <span className="text-xs font-medium uppercase text-muted-foreground">Add link</span>
                <div className="flex flex-wrap items-center gap-2">
                  <Select
                    aria-label="Relation"
                    className="w-[150px]"
                    selectedKey={linkRel}
                    onSelectionChange={(k) => setLinkRel(String(k))}
                  >
                    <Select.Trigger>
                      <Select.Value />
                      <Select.Indicator />
                    </Select.Trigger>
                    <Select.Popover>
                      <ListBox>
                        {RELS.map((r) => (
                          <ListBox.Item key={r} id={r} textValue={r}>
                            {r}
                            <ListBox.ItemIndicator />
                          </ListBox.Item>
                        ))}
                      </ListBox>
                    </Select.Popover>
                  </Select>
                  <TextField aria-label="Target memory id" className="flex-1" value={linkTo} onChange={setLinkTo}>
                    <Input className="font-mono text-xs" placeholder="mem_target_id" />
                  </TextField>
                  <Button isDisabled={busy || !linkTo.trim()} size="sm" variant="secondary" onPress={addLink}>
                    Link
                  </Button>
                </div>
              </div>
            </Modal.Body>
            <Modal.Footer className="flex-wrap">
              <Button isDisabled={busy} size="sm" variant="danger-soft" onPress={remove}>
                Delete
              </Button>
              <div className="flex-1" />
              <Button isDisabled={busy} size="sm" variant="secondary" onPress={() => verify("dispute")}>
                Dispute
              </Button>
              <Button isDisabled={busy} size="sm" onPress={() => verify("confirm")}>
                Confirm
              </Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}
