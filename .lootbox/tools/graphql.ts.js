export async function query(args) {
  const response = await fetch(args.endpoint, {
    method: "POST",
    headers: {
      "Content-Type": "application/json"
    },
    body: JSON.stringify({
      query: args.query,
      variables: args.variables
    })
  });
  return response.json();
}
export function introspect(args) {
  return query({
    endpoint: args.endpoint,
    query: `
        query IntrospectionQuery {
          __schema {
            types {
              name
              fields {
                name
                type { name kind ofType { name kind } }
              }
            }
          }
        }
      `
  });
}
//# sourceMappingURL=data:application/json;base64,eyJ2ZXJzaW9uIjozLCJzb3VyY2VzIjpbImZpbGU6Ly8vcHJpdmF0ZS92YXIvZm9sZGVycy9rOS9rMmtzcnRrMTc0ZzRsbWJjbmZ2bjR4djQwMDAwZ3AvVC9vcGVuY29kZS9sb290Ym94LXVwc3RyZWFtLy5sb290Ym94L3Rvb2xzL2dyYXBocWwudHMiXSwic291cmNlc0NvbnRlbnQiOlsiZXhwb3J0IGFzeW5jIGZ1bmN0aW9uIHF1ZXJ5KGFyZ3M6IHtcbiAgZW5kcG9pbnQ6IHN0cmluZztcbiAgcXVlcnk6IHN0cmluZztcbiAgdmFyaWFibGVzPzogUmVjb3JkPHN0cmluZywgdW5rbm93bj47XG59KSB7XG4gIGNvbnN0IHJlc3BvbnNlID0gYXdhaXQgZmV0Y2goYXJncy5lbmRwb2ludCwge1xuICAgIG1ldGhvZDogXCJQT1NUXCIsXG4gICAgaGVhZGVyczogeyBcIkNvbnRlbnQtVHlwZVwiOiBcImFwcGxpY2F0aW9uL2pzb25cIiB9LFxuICAgIGJvZHk6IEpTT04uc3RyaW5naWZ5KHtcbiAgICAgIHF1ZXJ5OiBhcmdzLnF1ZXJ5LFxuICAgICAgdmFyaWFibGVzOiBhcmdzLnZhcmlhYmxlcyxcbiAgICB9KSxcbiAgfSk7XG4gIHJldHVybiByZXNwb25zZS5qc29uKCk7XG59XG5cbmV4cG9ydCBmdW5jdGlvbiBpbnRyb3NwZWN0KGFyZ3M6IHsgZW5kcG9pbnQ6IHN0cmluZyB9KSB7XG4gIHJldHVybiBxdWVyeSh7XG4gICAgZW5kcG9pbnQ6IGFyZ3MuZW5kcG9pbnQsXG4gICAgcXVlcnk6IGBcbiAgICAgICAgcXVlcnkgSW50cm9zcGVjdGlvblF1ZXJ5IHtcbiAgICAgICAgICBfX3NjaGVtYSB7XG4gICAgICAgICAgICB0eXBlcyB7XG4gICAgICAgICAgICAgIG5hbWVcbiAgICAgICAgICAgICAgZmllbGRzIHtcbiAgICAgICAgICAgICAgICBuYW1lXG4gICAgICAgICAgICAgICAgdHlwZSB7IG5hbWUga2luZCBvZlR5cGUgeyBuYW1lIGtpbmQgfSB9XG4gICAgICAgICAgICAgIH1cbiAgICAgICAgICAgIH1cbiAgICAgICAgICB9XG4gICAgICAgIH1cbiAgICAgIGAsXG4gIH0pO1xufVxuIl0sIm5hbWVzIjpbXSwibWFwcGluZ3MiOiJBQUFBLE9BQU8sZUFBZSxNQUFNLElBSTNCO0VBQ0MsTUFBTSxXQUFXLE1BQU0sTUFBTSxLQUFLLFFBQVEsRUFBRTtJQUMxQyxRQUFRO0lBQ1IsU0FBUztNQUFFLGdCQUFnQjtJQUFtQjtJQUM5QyxNQUFNLEtBQUssU0FBUyxDQUFDO01BQ25CLE9BQU8sS0FBSyxLQUFLO01BQ2pCLFdBQVcsS0FBSyxTQUFTO0lBQzNCO0VBQ0Y7RUFDQSxPQUFPLFNBQVMsSUFBSTtBQUN0QjtBQUVBLE9BQU8sU0FBUyxXQUFXLElBQTBCO0VBQ25ELE9BQU8sTUFBTTtJQUNYLFVBQVUsS0FBSyxRQUFRO0lBQ3ZCLE9BQU8sQ0FBQzs7Ozs7Ozs7Ozs7O01BWU4sQ0FBQztFQUNMO0FBQ0YifQ==
// denoCacheMetadata=13425463841990573670,11494847700248121811