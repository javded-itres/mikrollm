FROM scratch
COPY dist/mikrollm /mikrollm
EXPOSE 4000
ENTRYPOINT ["/mikrollm"]
CMD ["-data", "/data", "-listen", ":4000"]
