{ pkgs ? import <nixpkgs> {} }:

pkgs.buildGoModule {
  pname = "pomodoro";
  version = "0.1.0";
  src = ./.;

  subPackages = [ "." ];

  vendorHash = "sha256-e7s6OSIJS7i12u01jvK8b+n2xY5r2+iKy/SlSybLTgU=";

  meta = with pkgs.lib; {
    description = "A TUI pomodoro timer written in Go";
    license = licenses.mit;
    mainProgram = "pomodoro";
    platforms = platforms.linux;
  };
}
