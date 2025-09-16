{pkgs, ... }: {
  packages = with pkgs; [
    buf
    ruff
  ];
  
  languages.go.enable = true;
  languages.python.enable = true;
  languages.python.uv.enable = true;

  scripts.bump-proto.exec = ''
    git -C proto fetch origin
    git -C proto checkout main
    git -C proto pull --ff-only
    git add proto
    git commit -m "⬆️ bump proto files"
    git push
  '';
  
  scripts.run.exec = "go run cmd/main.go";

  scripts.regen.exec = "rm -rf ./internal/gen; buf generate";

  dotenv.enable = true;

  env.UV_CACHE_DIR = ".uv-cache";
}
