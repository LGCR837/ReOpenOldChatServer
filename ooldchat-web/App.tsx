import React from 'react';
import { 
  MessageSquare, 
  Smartphone, 
  Zap, 
  Download, 
  Settings, 
  Users, 
  ShieldCheck, 
  Globe,
  BatteryCharging,
  Cpu,
  ArrowRight
} from 'lucide-react';
import LiveTile from './components/LiveTile';
import PivotHeader from './components/PivotHeader';
import { TileSize } from './types';

const App: React.FC = () => {

  const handleDownload = () => {
    alert("正在跳转到下载页面...");
  };

  return (
    <div className="min-h-screen bg-black text-white selection:bg-metro-accent selection:text-white pb-20 overflow-x-hidden">
      
      {/* Status Bar Imitation */}
      <div className="w-full h-6 flex justify-end items-center px-4 space-x-4 text-xs font-light text-gray-300 select-none cursor-default">
        <span>4G</span>
        <span>100%</span>
        <span>12:00</span>
      </div>

      <div className="max-w-2xl mx-auto px-4 pt-8">
        
        {/* Header Section */}
        <PivotHeader title="旧聊" subtitle="应用程序" />

        {/* Tiles Grid - Reduced gap for tighter WP8 feel */}
        <div className="grid grid-cols-4 gap-2 auto-rows-min animate-[pageIn_0.8s_ease-out_forwards]">
          
          {/* Main Feature: Compatibility */}
          <LiveTile
            size={TileSize.WIDE}
            title="极致兼容"
            bgColor="bg-[#0050ef]"
            icon={<Smartphone size={48} strokeWidth={1.5} />}
            count="Android 4.0+"
            delay={100}
            contentBack={
              <div className="flex flex-col items-center text-center">
                <p className="text-lg font-light mb-2">老机型焕发新生</p>
                <p className="text-xs opacity-80">支持 API Level 14+ <br/> 2012年的手机也能跑</p>
              </div>
            }
          />

          {/* Feature: Lightweight */}
          <LiveTile
            size={TileSize.MEDIUM}
            title="极速轻量"
            bgColor="bg-[#a4c400]" // Lime
            icon={<Zap size={40} strokeWidth={1.5} />}
            count="< 5MB"
            delay={200}
            contentBack={
               <div className="flex flex-col items-center">
                  <Cpu size={32} className="mb-2 opacity-80"/>
                  <span className="text-sm font-bold">零后台唤醒</span>
                  <span className="text-xs">省电 省内存</span>
               </div>
            }
          />

          {/* Feature: Fast Updates */}
          <LiveTile
            size={TileSize.MEDIUM}
            title="快速迭代"
            bgColor="bg-[#d80073]" // Magenta
            icon={<Settings size={40} strokeWidth={1.5} />}
            delay={300}
            contentBack={
              <div className="flex flex-col items-center text-center p-2">
                 <p className="text-sm">每周构建</p>
                 <p className="text-xs opacity-75 mt-2">不仅修Bug</p>
                 <p className="text-xs opacity-75">还加新功能</p>
              </div>
            }
          />

          {/* Feature: Community */}
          <LiveTile
            size={TileSize.SMALL}
            title="社区"
            bgColor="bg-[#f0a30a]" // Amber
            icon={<Users size={24} />}
            count="99+"
            delay={400}
            liveEffect={false}
          />

           {/* Feature: Privacy */}
           <LiveTile
            size={TileSize.SMALL}
            title="隐私"
            bgColor="bg-[#60a917]" // Green
            icon={<ShieldCheck size={24} />}
            delay={450}
            contentBack={<span className="text-xs font-bold">端对端加密</span>}
          />

          {/* Feature: Battery */}
          <LiveTile
            size={TileSize.SMALL}
            title="省电"
            bgColor="bg-[#1ba1e2]" // Cyan
            icon={<BatteryCharging size={24} />}
            delay={500}
            liveEffect={false}
          />

           {/* Feature: Global */}
           <LiveTile
            size={TileSize.SMALL}
            title="网络"
            bgColor="bg-[#6d8764]" // Olive
            icon={<Globe size={24} />}
            delay={550}
            contentBack={<span className="text-xs">弱网优化</span>}
          />

          {/* Main Action: Download */}
          <LiveTile
            size={TileSize.WIDE}
            bgColor="bg-[#aa00ff]" // Violet
            title="立即下载"
            icon={<Download size={48} strokeWidth={1.5} />}
            onClick={handleDownload}
            delay={600}
            liveEffect={false}
            contentBack={
                <div className="flex items-center space-x-2">
                    <span className="text-xl font-bold">获取 APK</span>
                    <ArrowRight />
                </div>
            }
          />

           {/* Decorative / Info */}
           <LiveTile
            size={TileSize.MEDIUM}
            title="最新版本"
            bgColor="bg-[#333333]" // Dark Grey
            icon={<MessageSquare size={40} strokeWidth={1.5} />}
            count="v2.1.0"
            delay={700}
            contentBack={
              <div className="text-left w-full px-2">
                <p className="text-xs text-gray-400 mb-1">更新日志:</p>
                <ul className="text-xs list-disc list-inside">
                  <li>修复闪退</li>
                  <li>新增夜间模式</li>
                  <li>优化启动速度</li>
                </ul>
              </div>
            }
          />

           {/* Empty Filler Tiles for aesthetics */}
           <LiveTile size={TileSize.SMALL} bgColor="bg-[#000] border-2 border-[#1f1f1f]" liveEffect={false} delay={800} />
           <LiveTile size={TileSize.SMALL} bgColor="bg-[#000] border-2 border-[#1f1f1f]" liveEffect={false} delay={850} />

        </div>

        {/* Footer Text */}
        <div className="mt-12 mb-8 text-gray-500 text-sm font-light animate-[pageIn_1s_ease-out_forwards]">
          <h3 className="uppercase tracking-widest mb-4 font-semibold text-metro-accent">关于我们</h3>
          <p className="mb-4">
            旧聊 (OldChat) 致力于为旧设备提供现代化的通讯体验。在这个软件越来越臃肿的时代，我们选择做减法。
          </p>
          <p>
            &copy; 2023 OldChat Project. Designed with Metro UI love.
          </p>
        </div>

      </div>
    </div>
  );
};

export default App;