# goutils
Golang utilities

## AI Promt to Create a Brief README.md


````
Write a brief README.md file for httpu Go package following these requirements:

1. **Package Description**: Write a concise description (1-2 sentences) explaining what the package does and its primary purpose

2. **Problem Statement**: Include a brief "Problem" or "Why" section that explains what problem this package solves or why it exists (1-2 sentences)

3. **Features Section**: List the main features/capabilities in bullet points, focusing on fundamental capabilities rather than exact implementation details. Keep each point under 72 characters per line

4. **Platform-Specific Logic**: If the package contains platform-specific code (build tags, OS-specific imports, different implementations for different platforms), add a "Platform Support" or "Compatibility" section noting this. If no platform-specific logic, no need for this section. Avoid phrases that tells that some platform-specific capabilities is missing

5. **Basic Usage Link**: If there is a `*_test.go` file containing a test function with "Basic" or "Usage" in its name (like `TestBasicUsage`, `TestUsage`, `ExampleBasic`, etc.), include a "Basic Usage" section with a link to that test file

6. **Line Length**: Ensure all lines are maximum 72 characters long

7. **Structure**: Use this format:
   ```markdown
   # Package Name

   Brief description of what the package does.

   ## Problem

   Brief explanation of what problem this solves.

   ## Features

   - **Basic Fundamental Capability 2 name 1-2 words** - Brief Capability 1 description
   - **Basic Fundamental Capability 2 name 1-2 words** - Brief Capability 2 description
   - **Basic Fundamental Capability 3 name 1-2 words** - Brief Capability 3 description

   ## Platform Support

   Notes about platform-specific behavior (if applicable).

   ## Basic Usage

   See [basic usage example](path/to/test_file.go)
   ```

8. **Style Guidelines**:
   - Use sentence capitalization for headings and list items
   - No periods at the end of list items
   - Add empty lines before lists
   - Keep descriptions focused and avoid unnecessary details
   - Focus on what developers need to know conceptually

Analyze the package structure, exported functions/types, test files, and any build tags or platform-specific code to determine the content. Focus on the fundamental value proposition rather than implementation specifics.

Notes about requirements 1 and 2:

- They must be conceptually distinct:
  - Description: What the package is and does (value and scope)
  - Problem: Why it exists; pain points without this package
- Avoid repeating key phrases or sentences across both sections
````
