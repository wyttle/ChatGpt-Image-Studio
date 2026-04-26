"use client";

import type { ImageMode } from "@/store/image-conversations";

export type ActiveImageTask = {
  conversationId: string;
  turnId: string;
  mode: ImageMode;
  count: number;
  variant: "standard" | "selection-edit";
  startedAt: number;
  remoteTaskId?: string;
};

type Listener = () => void;

const activeTasks = new Map<string, ActiveImageTask>();
const listeners = new Set<Listener>();

function getTaskKey(conversationId: string, turnId: string) {
  return `${conversationId}:${turnId}`;
}

function notifyListeners() {
  listeners.forEach((listener) => listener());
}

export function startImageTask(task: ActiveImageTask) {
  activeTasks.set(getTaskKey(task.conversationId, task.turnId), task);
  notifyListeners();
}

export function finishImageTask(conversationId: string, turnId: string) {
  activeTasks.delete(getTaskKey(conversationId, turnId));
  notifyListeners();
}

export function isImageTaskActive(conversationId: string, turnId: string) {
  return activeTasks.has(getTaskKey(conversationId, turnId));
}

export function listActiveImageTasks() {
  return Array.from(activeTasks.values()).sort((a, b) => b.startedAt - a.startedAt);
}

export function subscribeImageTasks(listener: Listener) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}
