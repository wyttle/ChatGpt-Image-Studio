"use client";

import { useCallback } from "react";
import { toast } from "sonner";

import {
  createImageEditTask,
  createImageGenerationTask,
  createImageUpscaleTask,
  editImage,
  generateImageWithOptions,
  upscaleImage,
  waitForImageTask,
  type ImageModel,
  type ImageQuality,
} from "@/lib/api";
import {
  finishImageTask,
  startImageTask,
} from "@/store/image-active-tasks";
import type {
  ImageConversation,
  ImageConversationTurn,
  ImageMode,
  StoredImage,
  StoredSourceImage,
} from "@/store/image-conversations";

import type { EditorTarget } from "./use-image-source-inputs";
import {
  buildConversationTitle,
  buildInpaintSourceReference,
  countFailures,
  createConversationTurn,
  createLoadingImages,
  dataUrlToFile,
  formatImageError,
  mergeResultImages,
  shouldFallbackSelectionEdit,
} from "../submit-utils";
import { buildSourceImageUrl } from "../view-utils";

type ActiveRequestState = {
  conversationId: string;
  turnId: string;
  mode: ImageMode;
  count: number;
  variant: "standard" | "selection-edit";
};

type UseImageSubmitOptions = {
  mode: ImageMode;
  imagePrompt: string;
  imageModel: ImageModel;
  imageSources: StoredSourceImage[];
  maskSource: StoredSourceImage | null;
  sourceImages: StoredSourceImage[];
  parsedCount: number;
  imageSize: string;
  imageQuality: ImageQuality;
  selectedConversationId: string | null;
  editorTarget: EditorTarget | null;
  isSubmitting: boolean;
  makeId: () => string;
  focusConversation: (conversationId: string) => void;
  closeSelectionEditor: () => void;
  setImagePrompt: (value: string) => void;
  setSourceImages: (value: StoredSourceImage[]) => void;
  setIsSubmitting: (value: boolean) => void;
  setActiveRequest: (value: ActiveRequestState | null) => void;
  setSubmitElapsedSeconds: (value: number) => void;
  setSubmitStartedAt: (value: number | null) => void;
  persistConversation: (conversation: ImageConversation) => Promise<void>;
  updateConversation: (
    conversationId: string,
    updater: (current: ImageConversation | null) => ImageConversation,
  ) => Promise<void>;
  resetComposer: (nextMode?: ImageMode) => void;
};

function buildConversationBase(conversationId: string, draftTurn: ImageConversationTurn): ImageConversation {
  return {
    id: conversationId,
    title: draftTurn.title,
    mode: draftTurn.mode,
    prompt: draftTurn.prompt,
    model: draftTurn.model,
    count: draftTurn.count,
    size: draftTurn.size,
    quality: draftTurn.quality,
    scale: draftTurn.scale,
    sourceImages: draftTurn.sourceImages,
    images: draftTurn.images,
    createdAt: draftTurn.createdAt,
    status: draftTurn.status,
    error: draftTurn.error,
    turns: [draftTurn],
  };
}

async function completeRemoteTask(
  taskId: string,
  conversationId: string,
  turnId: string,
  expectedCount: number,
  updateConversation: UseImageSubmitOptions["updateConversation"],
  draftTurn: ImageConversationTurn,
) {
  const data = await waitForImageTask(taskId);
  const resultItems = mergeResultImages(turnId, data.data || [], expectedCount);
  const failedCount = countFailures(resultItems);
  await updateConversation(conversationId, (current) => ({
    ...(current ?? buildConversationBase(conversationId, draftTurn)),
    turns: (current?.turns ?? [draftTurn]).map((item) =>
      item.id === turnId
        ? {
            ...item,
            images: resultItems,
            status: failedCount > 0 ? "error" : "success",
            error: failedCount > 0 ? `其中 ${failedCount} 张处理失败` : undefined,
          }
        : item,
    ),
  }));
  return failedCount;
}

function buildSourceReference(payload: {
  id: string;
  role: "image" | "mask";
  name: string;
  url: string;
}): StoredSourceImage {
  if (payload.url.startsWith("data:")) {
    return {
      id: payload.id,
      role: payload.role,
      name: payload.name,
      dataUrl: payload.url,
    };
  }
  return {
    id: payload.id,
    role: payload.role,
    name: payload.name,
    url: payload.url,
  };
}

export function useImageSubmit({
  mode,
  imagePrompt,
  imageModel,
  imageSources,
  maskSource,
  sourceImages,
  parsedCount,
  imageSize,
  imageQuality,
  selectedConversationId,
  editorTarget,
  isSubmitting,
  makeId,
  focusConversation,
  closeSelectionEditor,
  setImagePrompt,
  setSourceImages,
  setIsSubmitting,
  setActiveRequest,
  setSubmitElapsedSeconds,
  setSubmitStartedAt,
  persistConversation,
  updateConversation,
  resetComposer,
}: UseImageSubmitOptions) {
  const handleSelectionEditSubmit = useCallback(async ({
    prompt,
    mask,
  }: {
    prompt: string;
    mask: {
      file: File;
      previewDataUrl: string;
    };
  }) => {
    if (!editorTarget) {
      return;
    }

    const sourceReference = editorTarget.image ? buildInpaintSourceReference(editorTarget.image) : undefined;
    const targetConversationId = editorTarget.conversationId ?? selectedConversationId;
    const conversationId = targetConversationId ?? makeId();
    const supportsEditableOutputOptions = editorTarget.image === null;
    const turnId = makeId();
    const now = new Date().toISOString();
    const draftTurn = createConversationTurn({
      turnId,
      title: buildConversationTitle("edit", prompt),
      mode: "edit",
      prompt,
      model: imageModel,
      count: 1,
      size: supportsEditableOutputOptions ? imageSize : undefined,
      quality: supportsEditableOutputOptions ? imageQuality : undefined,
      sourceImages: [
        buildSourceReference({
          id: makeId(),
          role: "image",
          name: editorTarget.imageName,
          url: editorTarget.sourceDataUrl,
        }),
        {
          id: makeId(),
          role: "mask",
          name: "mask.png",
          dataUrl: mask.previewDataUrl,
        },
      ],
      images: createLoadingImages(1, turnId),
      createdAt: now,
      status: "generating",
    });

    const startedAt = Date.now();
    setIsSubmitting(true);
    setActiveRequest({
      conversationId,
      turnId,
      mode: "edit",
      count: 1,
      variant: "selection-edit",
    });
    setSubmitElapsedSeconds(0);
    setSubmitStartedAt(startedAt);
    focusConversation(conversationId);
    setImagePrompt("");
    setSourceImages([]);
    closeSelectionEditor();
    startImageTask({
      conversationId,
      turnId,
      mode: "edit",
      count: 1,
      variant: "selection-edit",
      startedAt,
    });

    try {
      if (targetConversationId) {
        await updateConversation(conversationId, (current) => {
          if (!current) {
            return buildConversationBase(conversationId, draftTurn);
          }
          return {
            ...current,
            turns: [...(current.turns ?? []), draftTurn],
          };
        });
      } else {
        await persistConversation(buildConversationBase(conversationId, draftTurn));
      }

      let fallbackImageFile = sourceReference
        ? null
        : await dataUrlToFile(editorTarget.sourceDataUrl, editorTarget.imageName || "source.png");
      let data;
      try {
        data = await editImage({
          prompt,
          images: fallbackImageFile ? [fallbackImageFile] : [],
          mask: mask.file,
          sourceReference,
          size: supportsEditableOutputOptions ? imageSize : undefined,
          quality: supportsEditableOutputOptions ? imageQuality : undefined,
          model: imageModel,
        });
      } catch (error) {
        if (!sourceReference || !shouldFallbackSelectionEdit(error)) {
          throw error;
        }
        fallbackImageFile =
          fallbackImageFile ??
          (await dataUrlToFile(editorTarget.sourceDataUrl, editorTarget.imageName || "source.png"));
        data = await editImage({
          prompt,
          images: [fallbackImageFile],
          mask: mask.file,
          size: supportsEditableOutputOptions ? imageSize : undefined,
          quality: supportsEditableOutputOptions ? imageQuality : undefined,
          model: imageModel,
        });
      }
      const resultItems = mergeResultImages(turnId, data.data || [], 1);
      const failedCount = countFailures(resultItems);

      await updateConversation(conversationId, (current) => ({
        ...(current ?? buildConversationBase(conversationId, draftTurn)),
        turns: (current?.turns ?? [draftTurn]).map((turn) =>
          turn.id === turnId
            ? {
                ...turn,
                images: resultItems,
                status: failedCount > 0 ? "error" : "success",
                error: failedCount > 0 ? `其中 ${failedCount} 张处理失败` : undefined,
              }
            : turn,
        ),
      }));

      if (failedCount > 0) {
        toast.error(`已返回结果，但有 ${failedCount} 张处理失败`);
      } else {
        toast.success("图片已按选区编辑");
      }
    } catch (error) {
      const message = formatImageError(error);
      await updateConversation(conversationId, (current) => ({
        ...(current ?? buildConversationBase(conversationId, draftTurn)),
        turns: (current?.turns ?? [draftTurn]).map((turn) =>
          turn.id === turnId
            ? {
                ...turn,
                status: "error",
                error: message,
                images: turn.images.map((image) => ({
                  ...image,
                  status: "error" as const,
                  error: message,
                })),
              }
            : turn,
        ),
      }));
      toast.error(message);
    } finally {
      finishImageTask(conversationId, turnId);
      setIsSubmitting(false);
      setActiveRequest(null);
      setSubmitStartedAt(null);
    }
  }, [
    closeSelectionEditor,
    editorTarget,
    focusConversation,
    imageModel,
    imageQuality,
    imageSize,
    makeId,
    persistConversation,
    selectedConversationId,
    setActiveRequest,
    setImagePrompt,
    setIsSubmitting,
    setSourceImages,
    setSubmitElapsedSeconds,
    setSubmitStartedAt,
    updateConversation,
  ]);

  const handleRetryTurn = useCallback(async (conversationId: string, turn: ImageConversationTurn) => {
    if (isSubmitting) {
      toast.error("正在处理中，请稍后再试");
      return;
    }

    const prompt = turn.prompt?.trim() ?? "";
    const turnMode = turn.mode || "generate";
    const turnSourceImages = Array.isArray(turn.sourceImages) ? turn.sourceImages : [];
      const turnImageSources = turnSourceImages.filter((item) => item.role === "image" && buildSourceImageUrl(item));
    const turnMaskSource = turnSourceImages.find((item) => item.role === "mask") ?? null;
    const turnQuality = turn.quality || "high";
    const expectedCount = Math.max(1, turn.count || 1);

    if (turnMode === "generate" && !prompt) {
      toast.error("该记录缺少提示词，无法重试");
      return;
    }
    if (turnMode === "edit" && turnImageSources.length === 0) {
      toast.error("该记录缺少源图，无法重试");
      return;
    }

    const turnId = turn.id;
    const now = new Date().toISOString();
    const draftTurn = createConversationTurn({
      turnId,
      title: buildConversationTitle(turnMode, prompt),
      mode: turnMode,
      prompt,
      model: turn.model,
      count: expectedCount,
      size: turn.size,
      quality: turnMode === "edit" || (turnMode === "generate" && turnImageSources.length === 0) ? turnQuality : undefined,
      sourceImages: turnSourceImages,
      images: createLoadingImages(expectedCount, turnId),
      createdAt: now,
      status: "generating",
    });

    const startedAt = Date.now();
    setIsSubmitting(true);
    setActiveRequest({
      conversationId,
      turnId,
      mode: turnMode,
      count: expectedCount,
      variant: "standard",
    });
    setSubmitElapsedSeconds(0);
    setSubmitStartedAt(startedAt);
    focusConversation(conversationId);
    startImageTask({
      conversationId,
      turnId,
      mode: turnMode,
      count: expectedCount,
      variant: "standard",
      startedAt,
    });

    try {
      await updateConversation(conversationId, (current) => ({
        ...(current ?? buildConversationBase(conversationId, draftTurn)),
        turns: current?.turns?.map((item) => (item.id === turnId ? draftTurn : item)) ?? [draftTurn],
      }));

      let taskId = "";
      if (turnMode === "generate") {
        if (turnImageSources.length > 0) {
          const files = await Promise.all(
            turnImageSources.map((item, index) => dataUrlToFile(buildSourceImageUrl(item), item.name || `reference-${index + 1}.png`)),
          );
          taskId = (await createImageEditTask({ prompt, images: files, size: turn.size, quality: turnQuality, model: turn.model })).task_id;
        } else {
          taskId = (await createImageGenerationTask(prompt, {
            model: turn.model,
            count: expectedCount,
            size: turn.size,
            quality: turnQuality,
          })).task_id;
        }
      }

      if (turnMode === "edit") {
        const files = await Promise.all(
          turnImageSources.map((item, index) => dataUrlToFile(buildSourceImageUrl(item), item.name || `image-${index + 1}.png`)),
        );
        const maskURL = turnMaskSource ? buildSourceImageUrl(turnMaskSource) : "";
        const mask = maskURL ? await dataUrlToFile(maskURL, turnMaskSource?.name || "mask.png") : null;
        taskId = (await createImageEditTask({ prompt, images: files, mask, size: turn.size, quality: turnQuality, model: turn.model })).task_id;
      }

      if (!taskId) {
        throw new Error("图片任务创建失败");
      }
      startImageTask({
        conversationId,
        turnId,
        mode: turnMode,
        count: expectedCount,
        variant: "standard",
        startedAt,
        remoteTaskId: taskId,
      });
      await updateConversation(conversationId, (current) => ({
        ...(current ?? buildConversationBase(conversationId, draftTurn)),
        turns: (current?.turns ?? [draftTurn]).map((item) =>
          item.id === turnId ? { ...item, remoteTaskId: taskId } : item,
        ),
      }));
      const failedCount = await completeRemoteTask(taskId, conversationId, turnId, expectedCount, updateConversation, {
        ...draftTurn,
        remoteTaskId: taskId,
      });

      if (failedCount > 0) {
        toast.error(`已返回结果，但有 ${failedCount} 张处理失败`);
      } else {
        toast.success(turnMode === "generate" ? "图片已生成" : "图片已编辑");
      }
    } catch (error) {
      const message = formatImageError(error);
      await updateConversation(conversationId, (current) => ({
        ...(current ?? buildConversationBase(conversationId, draftTurn)),
        turns: (current?.turns ?? [draftTurn]).map((item) =>
          item.id === turnId
            ? {
                ...item,
                status: "error",
                error: message,
                images: item.images.map((image) => ({
                  ...image,
                  status: "error" as const,
                  error: message,
                })),
              }
            : item,
        ),
      }));
      toast.error(message);
    } finally {
      finishImageTask(conversationId, turnId);
      setIsSubmitting(false);
      setActiveRequest(null);
      setSubmitStartedAt(null);
    }
  }, [focusConversation, isSubmitting, setActiveRequest, setIsSubmitting, setSubmitElapsedSeconds, setSubmitStartedAt, updateConversation]);

  const handleSubmit = useCallback(async () => {
    const prompt = imagePrompt.trim();
    if (mode === "generate" && !prompt) {
      toast.error("请输入提示词");
      return;
    }
    if (mode === "edit" && imageSources.length === 0) {
      toast.error("编辑模式至少需要一张源图");
      return;
    }
    if (mode === "edit" && !prompt) {
      toast.error("编辑模式需要提示词");
      return;
    }
    const conversationId = selectedConversationId ?? makeId();
    const turnId = makeId();
    const now = new Date().toISOString();
    const expectedCount = mode === "generate" && imageSources.length === 0 ? parsedCount : 1;
    const draftTurn = createConversationTurn({
      turnId,
      title: buildConversationTitle(mode, prompt),
      mode,
      prompt,
      model: imageModel,
      count: expectedCount,
      size: mode === "generate" || mode === "edit" ? imageSize : undefined,
      quality: mode === "edit" || (mode === "generate" && imageSources.length === 0) ? imageQuality : undefined,
      sourceImages,
      images: createLoadingImages(expectedCount, turnId),
      createdAt: now,
      status: "generating",
    });

    const startedAt = Date.now();
    setIsSubmitting(true);
    setActiveRequest({
      conversationId,
      turnId,
      mode,
      count: expectedCount,
      variant: "standard",
    });
    setSubmitElapsedSeconds(0);
    setSubmitStartedAt(startedAt);
    focusConversation(conversationId);
    setImagePrompt("");
    setSourceImages([]);
    startImageTask({
      conversationId,
      turnId,
      mode,
      count: expectedCount,
      variant: "standard",
      startedAt,
    });

    try {
      if (selectedConversationId) {
        await updateConversation(conversationId, (current) => ({
          ...(current ?? buildConversationBase(conversationId, draftTurn)),
          turns: [...(current?.turns ?? []), draftTurn],
        }));
      } else {
        await persistConversation(buildConversationBase(conversationId, draftTurn));
      }

      let taskId = "";
      if (mode === "generate") {
        if (imageSources.length > 0) {
          const files = await Promise.all(
            imageSources.map((item, index) => dataUrlToFile(buildSourceImageUrl(item), item.name || `reference-${index + 1}.png`)),
          );
          taskId = (await createImageEditTask({ prompt, images: files, size: imageSize, quality: imageQuality, model: imageModel })).task_id;
        } else {
          taskId = (await createImageGenerationTask(prompt, {
            model: imageModel,
            count: parsedCount,
            size: imageSize,
            quality: imageQuality,
          })).task_id;
        }
      }

      if (mode === "edit") {
        const files = await Promise.all(
          imageSources.map((item, index) => dataUrlToFile(buildSourceImageUrl(item), item.name || `image-${index + 1}.png`)),
        );
        const maskURL = maskSource ? buildSourceImageUrl(maskSource) : "";
        const mask = maskURL ? await dataUrlToFile(maskURL, maskSource?.name || "mask.png") : null;
        taskId = (await createImageEditTask({ prompt, images: files, mask, size: imageSize, quality: imageQuality, model: imageModel })).task_id;
      }

      if (mode === "upscale") {
        const file = await dataUrlToFile(buildSourceImageUrl(imageSources[0]), imageSources[0].name || "upscale.png");
        taskId = (await createImageUpscaleTask({ image: file, prompt, scale: imageSize, model: imageModel })).task_id;
      }

      if (!taskId) {
        throw new Error("图片任务创建失败");
      }
      startImageTask({
        conversationId,
        turnId,
        mode,
        count: expectedCount,
        variant: "standard",
        startedAt,
        remoteTaskId: taskId,
      });
      await updateConversation(conversationId, (current) => ({
        ...(current ?? buildConversationBase(conversationId, draftTurn)),
        turns: (current?.turns ?? [draftTurn]).map((turn) =>
          turn.id === turnId ? { ...turn, remoteTaskId: taskId } : turn,
        ),
      }));
      const failedCount = await completeRemoteTask(taskId, conversationId, turnId, expectedCount, updateConversation, {
        ...draftTurn,
        remoteTaskId: taskId,
      });

      resetComposer(mode === "generate" ? "generate" : "edit");
      if (failedCount > 0) {
        toast.error(`已返回结果，但有 ${failedCount} 张处理失败`);
      } else {
        toast.success(
          mode === "generate"
            ? imageSources.length > 0
              ? "参考图生成已完成"
              : "图片已生成"
            : "图片已编辑",
        );
      }
    } catch (error) {
      const message = formatImageError(error);
      await updateConversation(conversationId, (current) => ({
        ...(current ?? buildConversationBase(conversationId, draftTurn)),
        turns: (current?.turns ?? [draftTurn]).map((turn) =>
          turn.id === turnId
            ? {
                ...turn,
                status: "error",
                error: message,
                images: turn.images.map((image) => ({
                  ...image,
                  status: "error" as const,
                  error: message,
                })),
              }
            : turn,
        ),
      }));
      toast.error(message);
    } finally {
      finishImageTask(conversationId, turnId);
      setIsSubmitting(false);
      setActiveRequest(null);
      setSubmitStartedAt(null);
    }
  }, [
    focusConversation,
    imageModel,
    imagePrompt,
    imageSources,
    makeId,
    maskSource,
    mode,
    imageSize,
    imageQuality,
    parsedCount,
    persistConversation,
    resetComposer,
    selectedConversationId,
    setActiveRequest,
    setImagePrompt,
    setIsSubmitting,
    setSourceImages,
    setSubmitElapsedSeconds,
    setSubmitStartedAt,
    sourceImages,
    updateConversation,
  ]);

  return {
    handleSelectionEditSubmit,
    handleRetryTurn,
    handleSubmit,
  };
}
