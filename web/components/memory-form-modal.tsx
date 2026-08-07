"use client";

import { useState } from "react";
import {
  Button,
  Description,
  Input,
  Label,
  Modal,
  TextArea,
  TextField,
  toast,
} from "@heroui/react";
import { api, type Memory } from "@/lib/api";

// Modal for creating a new memory or editing an existing one.
export function MemoryFormModal({
  memory,
  isOpen,
  onOpenChange,
  onSaved,
}: {
  memory?: Memory | null; // undefined/null = create
  isOpen: boolean;
  onOpenChange: (open: boolean) => void;
  onSaved: () => void;
}) {
  const editing = Boolean(memory);
  const [content, setContent] = useState(memory?.content ?? "");
  const [tags, setTags] = useState(memory?.tags?.join(", ") ?? "");
  const [namespace, setNamespace] = useState(memory?.namespace ?? "default");
  const [ttl, setTtl] = useState("");
  const [busy, setBusy] = useState(false);

  async function save() {
    if (!content.trim()) return;
    setBusy(true);
    try {
      const tagList = tags.split(",").map((t) => t.trim()).filter(Boolean);
      if (editing && memory) {
        await api.update(memory.id, content, tagList);
        toast("Memory updated", { variant: "success" });
      } else {
        await api.add(content, tagList, namespace, ttl);
        toast("Memory stored", { variant: "success" });
      }
      onOpenChange(false);
      onSaved();
    } catch (e) {
      toast(e instanceof Error ? e.message : "Failed to save", { variant: "danger" });
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal isOpen={isOpen} onOpenChange={onOpenChange}>
      <Modal.Backdrop>
        <Modal.Container>
          <Modal.Dialog className="sm:max-w-[480px]">
            <Modal.CloseTrigger />
            <Modal.Header>
              <Modal.Heading>{editing ? "Edit memory" : "Add memory"}</Modal.Heading>
            </Modal.Header>
            <Modal.Body className="flex flex-col gap-4">
              <TextField isRequired name="content" value={content} onChange={setContent}>
                <Label>Content</Label>
                <TextArea placeholder="Deploys run at 6pm UTC…" rows={4} />
              </TextField>
              <TextField name="tags" value={tags} onChange={setTags}>
                <Label>Tags</Label>
                <Input placeholder="ops, deploy" />
                <Description>Comma separated</Description>
              </TextField>
              {!editing && (
                <div className="flex gap-3">
                  <TextField className="flex-1" name="namespace" value={namespace} onChange={setNamespace}>
                    <Label>Namespace</Label>
                    <Input placeholder="default" />
                  </TextField>
                  <TextField className="flex-1" name="ttl" value={ttl} onChange={setTtl}>
                    <Label>TTL</Label>
                    <Input placeholder="7d (optional)" />
                  </TextField>
                </div>
              )}
            </Modal.Body>
            <Modal.Footer>
              <Button variant="secondary" slot="close">
                Cancel
              </Button>
              <Button isDisabled={busy || !content.trim()} onPress={save}>
                {editing ? "Save changes" : "Store memory"}
              </Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}
