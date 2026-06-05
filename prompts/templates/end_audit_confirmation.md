你调用了 end_audit，但仍有 unseen/reviewing 文件。只有在 todo、flow、变量排查都已经闭合后，才允许用“剩余文件无价值”作为结束理由。请先确认这些文件是否确实无需继续审计。

!{files}

如果这些文件已经确认不需要继续审计，请先简短说明理由，然后再次调用 end_audit；只有当当前没有 pending todo、没有 tracking/suspicious 的 flow/变量时，第二次调用才会允许结束。如果还需要审计，请继续调用 read_file/search_content/file_review_update。
