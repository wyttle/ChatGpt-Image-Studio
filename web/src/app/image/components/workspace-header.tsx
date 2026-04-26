"use client";

import { PanelLeftClose, PanelLeftOpen } from "lucide-react";

import { Button } from "@/components/ui/button";
type WorkspaceHeaderProps = {
  historyCollapsed: boolean;
  selectedConversationTitle?: string | null;
  onToggleHistory: () => void;
};

export function WorkspaceHeader({
  historyCollapsed,
  selectedConversationTitle,
  onToggleHistory,
}: WorkspaceHeaderProps) {
  return (
    <div className="hidden border-b border-stone-200/80 px-5 py-4 sm:px-6 lg:block">
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <Button
            type="button"
            variant="outline"
            className="h-10 rounded-full border-stone-200 bg-white px-4 text-stone-700 shadow-none"
            onClick={onToggleHistory}
          >
            {historyCollapsed ? <PanelLeftOpen className="size-4" /> : <PanelLeftClose className="size-4" />}
            {historyCollapsed ? "展开历史" : "收起历史"}
          </Button>
          <h1 className="text-xl font-semibold tracking-tight text-stone-950 sm:text-[22px]">图片工作台</h1>
          {selectedConversationTitle ? (
            <span className="truncate rounded-full bg-stone-100 px-3 py-1 text-xs font-medium text-stone-600">
              {selectedConversationTitle}
            </span>
          ) : null}
        </div>
      </div>
    </div>
  );
}
