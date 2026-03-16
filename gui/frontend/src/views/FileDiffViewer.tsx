// File Diff Viewer: side-by-side diff of Meeseeks output with pipeline status.
// See Architecture Section 26.2.
import React from "react";

interface DiffFile {
  path: string;
  operation: string;
  content: string;
}

interface Props {
  taskId: string;
  files: DiffFile[];
  pipelineStatus: string;
}

export const FileDiffViewer: React.FC<Props> = ({ taskId, files, pipelineStatus }) => {
  return (
    <div className="view file-diff">
      <h2>File Diff: {taskId}</h2>
      <div className="pipeline-status">Pipeline: {pipelineStatus}</div>
      {files.length === 0 ? (
        <p>No files to display.</p>
      ) : (
        files.map((file) => (
          <div key={file.path} className="diff-file">
            <div className="diff-header">
              <span className="diff-op">{file.operation}</span>
              <span className="diff-path">{file.path}</span>
            </div>
            <pre className="diff-content">{file.content}</pre>
          </div>
        ))
      )}
    </div>
  );
};
