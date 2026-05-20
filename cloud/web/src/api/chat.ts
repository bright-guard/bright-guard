import { api } from "./client";
import type {
  ChatCreateSessionResp,
  ChatPostResp,
  ChatSessionListResp,
  ChatThreadResp,
} from "./types";

export function createChatSession(orgId: string) {
  return api<ChatCreateSessionResp>(
    `/api/orgs/${orgId}/chat/sessions`,
    { method: "POST", body: "{}" },
  );
}

export function listChatSessions(orgId: string) {
  return api<ChatSessionListResp>(`/api/orgs/${orgId}/chat/sessions`);
}

export function getChatSession(orgId: string, sessionId: string) {
  return api<ChatThreadResp>(
    `/api/orgs/${orgId}/chat/sessions/${sessionId}`,
  );
}

export function deleteChatSession(orgId: string, sessionId: string) {
  return api<void>(`/api/orgs/${orgId}/chat/sessions/${sessionId}`, {
    method: "DELETE",
  });
}

export function postChatMessage(
  orgId: string,
  sessionId: string,
  text: string,
) {
  return api<ChatPostResp>(
    `/api/orgs/${orgId}/chat/sessions/${sessionId}/messages`,
    { method: "POST", body: JSON.stringify({ text }) },
  );
}
