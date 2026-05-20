import React from "react";
import ReactDOM from "react-dom/client";
import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";

import "./index.css";
import { AuthProvider } from "./auth/AuthContext";
import { ProtectedRoute } from "./auth/ProtectedRoute";
import LoginPage from "./pages/LoginPage";
import OnboardingPage from "./pages/OnboardingPage";
import AppShell from "./pages/AppShell";
import OverviewPage from "./pages/OverviewPage";
import GatewaysPage from "./pages/GatewaysPage";
import GatewayDetailPage from "./pages/GatewayDetailPage";
import MCPServersPage from "./pages/MCPServersPage";
import MCPServerDetailPage from "./pages/MCPServerDetailPage";
import ConnectionsPage from "./pages/ConnectionsPage";
import ConnectionDetailPage from "./pages/ConnectionDetailPage";
import ActivityPage from "./pages/ActivityPage";
import CallersPage from "./pages/CallersPage";
import CallerDetailPage from "./pages/CallerDetailPage";
import DevicePage from "./pages/DevicePage";
import SessionsPage from "./pages/SessionsPage";
import MembersPage from "./pages/MembersPage";
import InvitationPage from "./pages/InvitationPage";
import PoliciesPage from "./pages/PoliciesPage";
import PolicyDetailPage from "./pages/PolicyDetailPage";
import AdminShell from "./admin/AdminShell";
import AdminOverviewPage from "./admin/OverviewPage";
import AdminUsersPage from "./admin/UsersPage";
import AdminUserDetailPage from "./admin/UserDetailPage";
import AdminOrgsPage from "./admin/OrgsPage";
import AdminOrgDetailPage from "./admin/OrgDetailPage";
import AdminAdminsPage from "./admin/AdminsPage";
import AdminAuditPage from "./admin/AuditPage";
import { RequirePlatformAdmin } from "./admin/RequirePlatformAdmin";
import DocsShell, { DocsIndexRedirect } from "./pages/DocsShell";
import DocsPage from "./pages/DocsPage";
import { HelpProvider } from "./components/HelpProvider";
import { ChatProvider } from "./components/ChatProvider";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <BrowserRouter>
      <AuthProvider>
        <HelpProvider>
        <ChatProvider>
        <Routes>
          <Route path="/" element={<Navigate to="/app" replace />} />
          <Route path="/login" element={<LoginPage />} />
          <Route
            path="/onboarding"
            element={
              <ProtectedRoute requireMembership={false}>
                <OnboardingPage />
              </ProtectedRoute>
            }
          />
          <Route
            path="/device"
            element={
              <ProtectedRoute>
                <DevicePage />
              </ProtectedRoute>
            }
          />
          <Route
            path="/app"
            element={
              <ProtectedRoute>
                <AppShell />
              </ProtectedRoute>
            }
          >
            <Route index element={<OverviewPage />} />
            <Route path="gateways" element={<GatewaysPage />} />
            <Route path="gateways/:id" element={<GatewayDetailPage />} />
            <Route path="mcp-servers" element={<MCPServersPage />} />
            <Route path="mcp-servers/:id" element={<MCPServerDetailPage />} />
            <Route path="mcp-connections" element={<ConnectionsPage />} />
            <Route path="mcp-connections/:id" element={<ConnectionDetailPage />} />
            <Route path="activity" element={<ActivityPage />} />
            <Route path="callers" element={<CallersPage />} />
            <Route path="callers/:id" element={<CallerDetailPage />} />
            <Route path="policies" element={<PoliciesPage />} />
            <Route path="policies/:id" element={<PolicyDetailPage />} />
            <Route path="settings/sessions" element={<SessionsPage />} />
            <Route path="settings/members" element={<MembersPage />} />
            <Route path="invitations/:id" element={<InvitationPage />} />
          </Route>
          <Route
            path="/docs"
            element={
              <ProtectedRoute>
                <DocsShell />
              </ProtectedRoute>
            }
          >
            <Route index element={<DocsIndexRedirect />} />
            <Route path="*" element={<DocsPage />} />
          </Route>
          <Route
            path="/admin"
            element={
              <RequirePlatformAdmin>
                <AdminShell />
              </RequirePlatformAdmin>
            }
          >
            <Route index element={<AdminOverviewPage />} />
            <Route path="users" element={<AdminUsersPage />} />
            <Route path="users/:id" element={<AdminUserDetailPage />} />
            <Route path="orgs" element={<AdminOrgsPage />} />
            <Route path="orgs/:id" element={<AdminOrgDetailPage />} />
            <Route path="admins" element={<AdminAdminsPage />} />
            <Route path="audit" element={<AdminAuditPage />} />
          </Route>
          <Route path="*" element={<Navigate to="/app" replace />} />
        </Routes>
        </ChatProvider>
        </HelpProvider>
      </AuthProvider>
    </BrowserRouter>
  </React.StrictMode>,
);
