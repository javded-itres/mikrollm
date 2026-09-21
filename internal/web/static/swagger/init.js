window.ui = SwaggerUIBundle({
  url: "/openapi.json",
  dom_id: "#swagger-ui",
  persistAuthorization: true,
  tryItOutEnabled: true,
  filter: true,
  displayRequestDuration: true,
  docExpansion: "list",
  defaultModelsExpandDepth: 1,
  deepLinking: true,
  syntaxHighlight: { activate: true, theme: "idea" },
  layout: "BaseLayout"
});
