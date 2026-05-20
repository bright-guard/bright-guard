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
import ActivityPage from "./pages/ActivityPage";
import DevicePage from "./pages/DevicePage";
import SessionsPage from "./pages/SessionsPage";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <BrowserRouter>
      <AuthProvider>
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
            <Route path="activity" element={<ActivityPage />} />
            <Route path="settings/sessions" element={<SessionsPage />} />
          </Route>
          <Route path="*" element={<Navigate to="/app" replace />} />
        </Routes>
      </AuthProvider>
    </BrowserRouter>
  </React.StrictMode>,
);
