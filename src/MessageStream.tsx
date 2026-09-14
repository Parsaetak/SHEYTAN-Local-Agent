import { memo, useEffect, useMemo, useRef, useState } from "react";

import type { Attachment, ChatMessage } from "./api";
import { useRuntimeStore } from "./store";

const MAX_RENDERED_MESSAGES = 200;

function formatBytes(bytes: number): string {
  if (bytes >= 1_048_576) {
    return `${(bytes / 1_048_576).toFixed(1)} MB`;
  }

  if (bytes >= 1024) {
    return `${(bytes / 1024).toFixed(1)} KB`;
  }

  return `${bytes} B`;
}

function AttachmentChip({
  attachment,
  onRemove,
}: {
  attachment: Pick<Attachment, "id" | "name" | "kind" | "size">;
  onRemove?: (id: string) => void;
}) {
  return (
    <span className={`attachment-chip kind-${attachment.kind}`}>
      <span className="attachment-chip-kind">{attachment.kind}</span>

      <span className="attachment-chip-name" title={attachment.name}>
        {attachment.name}
      </span>

      <span className="attachment-chip-size">
        {formatBytes(attachment.size)}
      </span>

      {onRemove ? (
        <button
          type="button"
          className="attachment-chip-remove"
          onClick={() => onRemove(attachment.id)}
          aria-label={`Remove ${attachment.name}`}
        >
          ×
        </button>
      ) : null}
    </span>
  );
}

const MessageBubble = memo(function MessageBubble({
  message,
  query,
}: {
  message: ChatMessage;
  query: string | null;
}) {
  const isUser = message.role === "user";

  // v1.1.4Z: recall feedback. 👍/👎 on an assistant reply steers the
  // recall engine's future relevance scoring (the backend steering has
  // existed since v1.0.6 — this is its first user-facing write path).
  const sendFeedback = useRuntimeStore((state) => state.sendFeedback);
  const [feedback, setFeedback] = useState<"liked" | "disliked" | null>(
    null,
  );
  const [feedbackError, setFeedbackError] = useState(false);

  const handleFeedback = (liked: boolean) => {
    if (!query || feedback) {
      return;
    }

    const next = liked ? "liked" : "disliked";

    setFeedback(next);

    sendFeedback(query, liked).catch(() => {
      // Optimistic verdict failed — revert so the user can retry.
      setFeedback(null);
      setFeedbackError(true);
    });
  };

  return (
    <article className={`message-row ${isUser ? "from-user" : "from-agent"}`}>
      <div className="message-avatar" aria-hidden="true">
        {isUser ? "U" : "S"}
      </div>

      <div className="message-bubble">
        {message.reasoning ? (
          <details className="message-reasoning">
            <summary>reasoning</summary>

            <p className="message-reasoning-body">{message.reasoning}</p>
          </details>
        ) : null}

        <p className="message-content">{message.content}</p>

        {message.attachments && message.attachments.length > 0 ? (
          <div className="message-attachments">
            {message.attachments.map((name) => (
              <span key={name} className="attachment-chip kind-text">
                <span className="attachment-chip-name" title={name}>
                  {name}
                </span>
              </span>
            ))}
          </div>
        ) : null}

        {!isUser && message.content && query ? (
          <div className="message-feedback" role="group" aria-label="Rate this reply for recall relevance">
            <button
              type="button"
              className={`feedback-button like ${feedback === "liked" ? "active" : ""}`}
              disabled={feedback !== null}
              onClick={() => handleFeedback(true)}
              aria-label="Helpful — prioritize similar past exchanges in recall"
              title="Helpful — recall will prioritize this exchange"
            >
              ◆
            </button>

            <button
              type="button"
              className={`feedback-button dislike ${feedback === "disliked" ? "active" : ""}`}
              disabled={feedback !== null}
              onClick={() => handleFeedback(false)}
              aria-label="Not helpful — deprioritize similar past exchanges"
              title="Not helpful — recall will deprioritize this exchange"
            >
              ◇
            </button>

            {feedbackError ? (
              <span className="feedback-error">couldn't save rating</span>
            ) : null}
          </div>
        ) : null}
      </div>
    </article>
  );
});

function StreamingBubble() {
  const streaming = useRuntimeStore((state) => state.streaming);

  if (!streaming) {
    return null;
  }

  return (
    <article className="message-row from-agent">
      <div className="message-avatar" aria-hidden="true">
        S
      </div>

      <div className="message-bubble streaming">
        {streaming.reasoning ? (
          <p className="message-reasoning-body">{streaming.reasoning}</p>
        ) : null}

        <p className="message-content">
          {streaming.content || "…"}

          <span className="stream-cursor" aria-hidden="true" />
        </p>
      </div>
    </article>
  );
}

function EmptyConversation() {
  const engineState = useRuntimeStore((state) => state.engine?.state);
  // v1.1.8: mode-aware empty state — Chat invites conversation, Agent
  // invites a task. Same stream, same runtime underneath.
  const mode = useRuntimeStore((state) => state.mode);
  const chat = mode === "chat";

  const engineLive =
    engineState === "ready" || engineState === "running" || engineState === "busy";

  return (
    <div className="conversation-empty">
      <div className="activity-empty-mark">✦</div>

      <strong>{chat ? "Ready when you are" : "Conversation ready"}</strong>

      <span>
        {!engineLive
          ? chat
            ? "Send a message below — the engine starts automatically."
            : "Send a task below — the engine starts automatically."
          : chat
            ? "Engine is live. Say hello, ask anything, or attach files."
            : "Engine is live. Send a task below to begin."}
      </span>
    </div>
  );
}

function MessageStream() {
  const messages = useRuntimeStore((state) => state.messages);
  const streaming = useRuntimeStore((state) => state.streaming);
  const running = useRuntimeStore((state) => state.running);
  const activity = useRuntimeStore((state) => state.activity);

  const endRef = useRef<HTMLDivElement | null>(null);

  const visibleMessages = useMemo(
    () =>
      messages.length > MAX_RENDERED_MESSAGES
        ? messages.slice(-MAX_RENDERED_MESSAGES)
        : messages,
    [messages],
  );

  // The user message immediately preceding each assistant reply is the
  // exchange's recall query — the backend derives the same capsule id.
  const messageQueries = useMemo(() => {
    const queries: (string | null)[] = [];
    let lastUser: string | null = null;

    for (const message of messages) {
      if (message.role === "user") {
        lastUser = message.content;
        queries.push(null);
      } else {
        queries.push(lastUser);
      }
    }

    return queries;
  }, [messages]);

  const queryWindow =
    messages.length > MAX_RENDERED_MESSAGES
      ? messageQueries.slice(-MAX_RENDERED_MESSAGES)
      : messageQueries;

  // Only the last ~40 activity entries are mirrored inline; the full
  // activity feed stays bounded in the store.
  const inlineActivity = useMemo(() => {
    const interesting = activity.filter(
      (item) =>
        item.type === "tool_start" ||
        item.type === "tool_end" ||
        item.type === "engine" ||
        item.type === "context",
    );

    return interesting.slice(-8);
  }, [activity]);

  useEffect(() => {
    const frame = requestAnimationFrame(() => {
      endRef.current?.scrollIntoView({
        behavior: "auto",
        block: "nearest",
      });
    });

    return () => cancelAnimationFrame(frame);
  }, [visibleMessages.length, streaming?.content, inlineActivity.length]);

  return (
    <div className="conversation-panel">
      <div className="conversation-stream">
        {visibleMessages.length === 0 && !streaming ? (
          <EmptyConversation />
        ) : (
          <>
            {visibleMessages.map((message, index) => (
              <MessageBubble
                key={`${index}-${message.role}-${message.at ?? ""}`}
                message={message}
                query={queryWindow[index] ?? null}
              />
            ))}

            {inlineActivity.length > 0 ? (
              <div className="conversation-activity" aria-label="Runtime activity">
                {inlineActivity.map((item) => (
                  <span key={item.id} className={`conversation-activity-item type-${item.type}`}>
                    <span className="conversation-activity-type">{item.type}</span>

                    <span className="conversation-activity-caption">
                      {typeof item.data.caption === "string"
                        ? item.data.caption
                        : item.type}
                    </span>
                  </span>
                ))}
              </div>
            ) : null}

            {running ? <StreamingBubble /> : null}
          </>
        )}

        <div ref={endRef} />
      </div>
    </div>
  );
}

export { AttachmentChip };

export default MessageStream;
