import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  transpilePackages: ["@pivox/primitives", "@pivox/ui", "@pivox/features"],
};

export default nextConfig;
